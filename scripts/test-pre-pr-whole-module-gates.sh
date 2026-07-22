#!/usr/bin/env bash
# Static regression tests for scripts/dev/pre-pr.sh whole-module gate scheduling.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/pre-pr.sh"
precommit_script="${repo_root}/scripts/dev/precommit-go.sh"

fail() {
	printf 'test-pre-pr-whole-module-gates: %s\n' "$*" >&2
	exit 1
}

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || \
		fail "missing ${label}: ${needle}"
}

require_precommit() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${precommit_script}" || \
		fail "missing ${label}: ${needle}"
}

reject() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${script}"; then
		fail "unexpected ${label}: ${needle}"
	fi
}

reject_precommit() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${precommit_script}"; then
		fail "unexpected ${label}: ${needle}"
	fi
}

[[ -f "${script}" ]] || fail "missing ${script}"
bash -n "${script}" || fail "pre-pr.sh has a syntax error"
[[ -f "${precommit_script}" ]] || fail "missing ${precommit_script}"
bash -n "${precommit_script}" || fail "precommit-go.sh has a syntax error"

cache_paths="$("${precommit_script}" cache-paths)" ||
	fail "precommit-go.sh cache-paths failed"
tool_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
worktree_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"
golangci_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^golangci_cache_dir=' | cut -d= -f2-)"
expected_tool_cache="$(git -C "${repo_root}" rev-parse --git-common-dir)/eshu-precommit"
expected_worktree_cache="$(git -C "${repo_root}" rev-parse --git-dir)/eshu-precommit"

[[ "${tool_cache_dir}" == "${expected_tool_cache}" ]] ||
	fail "tool cache = ${tool_cache_dir}, want ${expected_tool_cache}"
[[ "${worktree_cache_dir}" == "${expected_worktree_cache}" ]] ||
	fail "worktree cache = ${worktree_cache_dir}, want ${expected_worktree_cache}"
[[ "${golangci_cache_dir}" == "${worktree_cache_dir}/golangci-lint" ]] ||
	fail "golangci cache = ${golangci_cache_dir}, want ${worktree_cache_dir}/golangci-lint"

temp_root="$(mktemp -d)"
trap 'rm -rf "${temp_root}"' EXIT
mini_repo="${temp_root}/repo"
linked_worktree="${temp_root}/linked"
git init -q "${mini_repo}"
mkdir -p "${mini_repo}/scripts/dev" "${mini_repo}/.github/workflows"
cp "${precommit_script}" "${mini_repo}/scripts/dev/precommit-go.sh"
printf '%s\n' 'run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2' > "${mini_repo}/.github/workflows/test.yml"
printf '%s\n' 'run: go install github.com/securego/gosec/v2/cmd/gosec@v2.27.1' > "${mini_repo}/.github/workflows/security-scan.yml"
git -C "${mini_repo}" add scripts .github
git -C "${mini_repo}" -c user.name=cache-test -c user.email=cache-test@example.invalid commit -qm init
git -C "${mini_repo}" worktree add --detach -q "${linked_worktree}" HEAD

main_paths="$(cd "${mini_repo}" && scripts/dev/precommit-go.sh cache-paths)"
linked_paths="$(cd "${linked_worktree}" && scripts/dev/precommit-go.sh cache-paths)"
main_tool_cache="$(printf '%s\n' "${main_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
linked_tool_cache="$(printf '%s\n' "${linked_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
main_worktree_cache="$(printf '%s\n' "${main_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"
linked_worktree_cache="$(printf '%s\n' "${linked_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"

[[ "${main_tool_cache}" == "${linked_tool_cache}" ]] ||
	fail "linked worktrees did not share the tool cache"
[[ "${main_worktree_cache}" != "${linked_worktree_cache}" ]] ||
	fail "linked worktrees unexpectedly shared mutable precommit state"

# Keep the production consumers tied to the isolated paths. Calculating the
# right directories is insufficient if a linter or report still writes to the
# shared tool cache.
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require_precommit "worktree-local stripped config" 'local out="${worktree_cache_dir}/golangci-nocustom.yml"'
# shellcheck disable=SC2016
require_precommit "worktree-local golangci cache" 'GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE:-${golangci_cache_dir}}" "$@"'
# shellcheck disable=SC2016
parallel_run_count="$(rg --fixed-strings -c -- '--allow-parallel-runners --config "${cfg}"' "${precommit_script}")"
[[ "${parallel_run_count}" == "2" ]] ||
	fail "parallel-runner flag count = ${parallel_run_count}, want 2 lint entrypoints"
# shellcheck disable=SC2016
require_precommit "worktree-local changed-package SARIF" 'out="${worktree_cache_dir}/gosec.sarif"'
# shellcheck disable=SC2016
require_precommit "worktree-local whole-module SARIF" 'out="${worktree_cache_dir}/gosec-all.sarif"'
# shellcheck disable=SC2016
reject_precommit "mutable SARIF in shared tool cache" 'out="${tool_cache_dir}/gosec'

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

printf 'PASS: pre-pr scheduling and worktree cache isolation are race-safe\n'
