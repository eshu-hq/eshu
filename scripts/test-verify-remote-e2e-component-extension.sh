#!/usr/bin/env bash
set -euo pipefail

# Self-test for the Scorecard component-extension proof verifier (#2126). It
# proves the verifier is well-formed and that it passes a good proof-artifact
# set while failing closed on a retrying workflow item, zero committed facts, or
# leaked host-local material. This runs locally with no Compose stack; the live
# run that produces real artifacts is the operator/CI gate.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-remote-e2e-component-extension.sh"
fixtures="${repo_root}/tests/fixtures/component_extension_proof"

die() {
	printf 'test-verify-remote-e2e-component-extension: %s\n' "$*" >&2
	exit 1
}

[[ -f "${verifier}" ]] || die "missing verifier: ${verifier}"
bash -n "${verifier}" || die "verifier failed bash syntax check"
command -v jq >/dev/null 2>&1 || die "jq is required"

# --list names every proof check without running anything.
list_log="$(bash "${verifier}" --list)"
for needle in "CLI inventory:" "deployed API/MCP:" "workflow:" "facts:" "provenance:" "redaction canary:"; do
	rg --fixed-strings --quiet "${needle}" < <(printf '%s\n' "${list_log}") \
		|| die "--list output missing ${needle}"
done

# Good artifacts pass.
bash "${verifier}" --artifacts "${fixtures}/good" >/dev/null \
	|| die "verifier rejected the good proof artifacts"

# A deployed readback with the wrong component identity must fail closed on
# every HTTP and MCP surface. This protects the proof from silently validating
# only the CLI/Postgres artifacts while ignoring a capability claim.
bad_readback="$(mktemp -d "${TMPDIR:-/tmp}/component-extension-bad-readback.XXXXXX")"
trap 'rm -rf "${bad_readback}"' EXIT
for response in api-inventory mcp-inventory api-diagnostics mcp-diagnostics; do
	cp "${fixtures}/good/"*.json "${bad_readback}/"
	if [[ "${response}" == *inventory ]]; then
		filter='.data.components[0].id = "dev.eshu.examples.wrong"'
	else
		filter='.data.component.id = "dev.eshu.examples.wrong"'
	fi
	jq "${filter}" "${bad_readback}/${response}.json" >"${bad_readback}/${response}.tmp"
	mv "${bad_readback}/${response}.tmp" "${bad_readback}/${response}.json"
	if bash "${verifier}" --artifacts "${bad_readback}" >/dev/null 2>&1; then
		die "verifier accepted a mismatched deployed response: ${response}"
	fi
done

# Each bad artifact set must fail closed.
for bad in bad_retry bad_no_facts bad_leak bad_provenance; do
	if bash "${verifier}" --artifacts "${fixtures}/${bad}" >/dev/null 2>&1; then
		die "verifier accepted bad artifacts: ${bad}"
	fi
done

printf 'component-extension proof verifier self-test passed\n'
