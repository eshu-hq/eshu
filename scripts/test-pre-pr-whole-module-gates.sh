#!/usr/bin/env bash
# Static regression tests for scripts/dev/pre-pr.sh whole-module gate scheduling.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/pre-pr.sh"

fail() {
	printf 'test-pre-pr-whole-module-gates: %s\n' "$*" >&2
	exit 1
}

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || \
		fail "missing ${label}: ${needle}"
}

reject() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${script}"; then
		fail "unexpected ${label}: ${needle}"
	fi
}

[[ -f "${script}" ]] || fail "missing ${script}"
bash -n "${script}" || fail "pre-pr.sh has a syntax error"

require "serial precommit lane" "run_precommit_gates_serial()"
require "captured gate helper" "capture_whole_module_gate()"
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require "fmt capture" 'capture_whole_module_gate "${tmpdir}" fmt "gofumpt (whole module)" step_fmt'
# shellcheck disable=SC2016
require "lint capture" 'capture_whole_module_gate "${tmpdir}" lint "golangci-lint (whole module)" step_lint'
# shellcheck disable=SC2016
require "build capture" 'capture_whole_module_gate "${tmpdir}" build "go build ./..." step_build'
# shellcheck disable=SC2016
require "vet capture" 'capture_whole_module_gate "${tmpdir}" vet "go vet ./..." step_vet'
# shellcheck disable=SC2016
require "stored duration readback" 'duration="$(cat "${tmpdir}/${n}.duration" 2>/dev/null || printf "0")"'

reject "shared parallel launcher state" "starts=()"
reject "wait-time duration accounting" 'SECONDS - starts[i]'

awk '
	/^run_precommit_gates_serial\(\)/ { in_func=1 }
	in_func && /capture_whole_module_gate .* fmt / { saw_fmt=NR }
	in_func && /capture_whole_module_gate .* lint / {
		if (saw_fmt == 0) {
			print "lint is captured before fmt in run_precommit_gates_serial" > "/dev/stderr"
			exit 1
		}
		saw_lint=NR
	}
	in_func && /^}/ { in_func=0 }
	END {
		if (saw_fmt == 0 || saw_lint == 0) {
			print "run_precommit_gates_serial must capture fmt then lint" > "/dev/stderr"
			exit 1
		}
	}
' "${script}" || fail "fmt/lint are not serialized in the precommit lane"

printf 'PASS: pre-pr whole-module gate scheduling is race-safe\n'
