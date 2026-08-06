#!/usr/bin/env bash
# Static contract test for verify-replay-tier.sh. The live verifier owns one
# shared NornicDB, so separate package test binaries must not mutate it in
# parallel.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-replay-tier.sh"
workflow="${repo_root}/.github/workflows/verify-replay-tier.yml"
ci_gates="${repo_root}/specs/ci-gates.v1.yaml"
prepr="${repo_root}/scripts/dev/pre-pr.sh"

fail() { printf 'test-verify-replay-tier: %s\n' "$*" >&2; exit 1; }

has_serialized_package_command() {
	rg --quiet \
		'^[[:space:]]*go test -p=1 \./internal/replay/offlinetier/ \./internal/reducer/ \\$' \
		"$1"
}

has_workflow_wiring() {
	rg --quiet "^[[:space:]]*- 'scripts/test-verify-replay-tier\\.sh'$" "$1" &&
		rg --quiet "^[[:space:]]*- 'scripts/dev/pre-pr\\.sh'$" "$1" &&
		rg --quiet '^[[:space:]]*run: bash scripts/test-verify-replay-tier\.sh$' "$1"
}

has_registry_wiring() {
	rg --quiet '^[[:space:]]*- "scripts/test-verify-replay-tier\.sh"$' "$1" &&
		rg --quiet '^[[:space:]]*- "scripts/dev/pre-pr\.sh"$' "$1" &&
		rg --quiet '^[[:space:]]*test_command: "bash scripts/test-verify-replay-tier\.sh"$' "$1"
}

replay_prepr_trigger() {
	sed -n '/^[[:space:]]*run_or_defer replay-tier \\$/ { n; p; q; }' "$1"
}

has_prepr_selector_parity() {
	local trigger
	trigger="$(replay_prepr_trigger "$1")"
	rg --quiet "^[[:space:]]+'\\^\\(.*\\)'[[:space:]]+\\\\$" <<<"${trigger}" || return 1
	for required_path in \
		'go/cmd/(ingester|projector)/' \
		'go/internal/(replay|reducer|storage/cypher|storage/nornicdb|projector|graph|runtime)/' \
		'testdata/cassettes/(replayoffline|replaydelta)/' \
		'scripts/(verify-replay-tier|test-verify-replay-tier)\.sh' \
		'scripts/dev/pre-pr\.sh' \
		'\.github/workflows/verify-replay-tier\.yml'; do
		rg --fixed-strings --quiet "${required_path}" <<<"${trigger}" || return 1
	done
}

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-replay-tier.sh must be executable"
bash -n "${script}" || fail "verify-replay-tier.sh has a syntax error"
[[ "$(wc -l <"${script}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "verify-replay-tier.sh must stay under 500 lines"

has_serialized_package_command "${script}" \
	|| fail "shared-graph package test binaries must run sequentially with go test -p=1"

[[ -f "${workflow}" ]] || fail "missing ${workflow}"
has_workflow_wiring "${workflow}" \
	|| fail "workflow must actively run and trigger on this contract test"
[[ -f "${ci_gates}" ]] || fail "missing ${ci_gates}"
has_registry_wiring "${ci_gates}" \
	|| fail "CI gate registry must actively trigger on and run this contract test"

[[ -f "${prepr}" ]] || fail "missing ${prepr}"
has_prepr_selector_parity "${prepr}" \
	|| fail "active pre-pr replay selector does not mirror the workflow and registry"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
sed 's/^[[:space:]]*go test -p=1 /# go test -p=1 /' "${script}" >"${tmp}/script"
if has_serialized_package_command "${tmp}/script"; then
	fail "commented-out package serialization must not satisfy the guard"
fi
sed -e "/^[[:space:]]*- 'scripts\\/test-verify-replay-tier\\.sh'$/s/^/# /" \
	-e '/^[[:space:]]*run: bash scripts\/test-verify-replay-tier\.sh$/s/^/# /' \
	"${workflow}" >"${tmp}/workflow"
if has_workflow_wiring "${tmp}/workflow"; then
	fail "commented-out workflow wiring must not satisfy the guard"
fi
sed -e '/^[[:space:]]*- "scripts\/test-verify-replay-tier\.sh"$/s/^/# /' \
	-e '/^[[:space:]]*test_command: "bash scripts\/test-verify-replay-tier\.sh"$/s/^/# /' \
	"${ci_gates}" >"${tmp}/ci-gates"
if has_registry_wiring "${tmp}/ci-gates"; then
	fail "commented-out registry wiring must not satisfy the guard"
fi
sed '/^[[:space:]]*run_or_defer replay-tier \\$/s/^/# /' \
	"${prepr}" >"${tmp}/pre-pr"
if has_prepr_selector_parity "${tmp}/pre-pr"; then
	fail "commented-out pre-pr selector must not satisfy the guard"
fi

printf 'test-verify-replay-tier: PASS\n'
