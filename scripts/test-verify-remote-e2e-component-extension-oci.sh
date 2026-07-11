#!/usr/bin/env bash
set -euo pipefail

# Self-test for the Scorecard component-extension OCI-adapter proof verifier
# (#1980, #1923). It proves the verifier is well-formed, passes a good OCI
# proof-artifact set, and fails closed when the run was not on the OCI adapter,
# the launched artifact was not digest-pinned, or host-local material leaked
# into the provenance. This runs locally with no Compose stack; the live run
# that produces real artifacts is the operator/CI gate.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-remote-e2e-component-extension-oci.sh"
fixtures="${repo_root}/tests/fixtures/component_extension_proof_oci"

die() {
	printf 'test-verify-remote-e2e-component-extension-oci: %s\n' "$*" >&2
	exit 1
}

[[ -f "${verifier}" ]] || die "missing verifier: ${verifier}"
bash -n "${verifier}" || die "verifier failed bash syntax check"

# --list names the OCI-specific proof checks without running anything.
list_log="$(bash "${verifier}" --list)"
for needle in "adapter=oci" "digest-pinned" "redaction canary:"; do
	rg --fixed-strings --quiet "${needle}" < <(printf '%s\n' "${list_log}") \
		|| die "--list output missing ${needle}"
done

# Good artifacts pass.
bash "${verifier}" --artifacts "${fixtures}/good" >/dev/null \
	|| die "verifier rejected the good OCI proof artifacts"

# Each bad artifact set must fail closed.
for bad in bad_process_adapter bad_floating_tag bad_leak_provenance; do
	if bash "${verifier}" --artifacts "${fixtures}/${bad}" >/dev/null 2>&1; then
		die "verifier accepted bad artifacts: ${bad}"
	fi
done

printf 'component-extension OCI proof verifier self-test passed\n'
