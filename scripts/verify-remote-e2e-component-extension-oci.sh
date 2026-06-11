#!/usr/bin/env bash
set -euo pipefail

# Verifier for the Scorecard component-extension OCI-adapter remote Compose
# proof (#1980, #1923). It layers the OCI-specific invariants on top of the
# shared component-extension verifier: the same inventory/workflow/facts proof
# plus a provenance record that proves the component ran through the OCI adapter
# against a real digest-pinned artifact. It operates on a recorded artifacts
# directory so it is deterministic and self-testable; the live run that produces
# those artifacts from a running Compose stack is the operator/CI gate.
#
# Required artifacts (in addition to the base verifier's):
#   provenance-oci.json   adapter=oci, digest-pinned oci_image, run provenance

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
base_verifier="${repo_root}/scripts/verify-remote-e2e-component-extension.sh"
list_only=false
artifacts_dir=""

usage() {
	cat <<USAGE
Usage: $(basename "$0") --artifacts <dir> [--list]

Verifies recorded Scorecard OCI-adapter component-extension proof artifacts:
  inventory.json        component-extensions API readback (shared verifier)
  workflow-items.json   component workflow item terminal states (shared verifier)
  facts.json            committed dev.eshu.examples.scorecard.* counts (shared verifier)
  provenance-oci.json   adapter=oci + digest-pinned artifact + run provenance

  --list   print the proof checks without running them
USAGE
}

die() {
	printf 'verify-remote-e2e-component-extension-oci: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--list) list_only=true; shift ;;
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v rg >/dev/null 2>&1 || die "rg is required"
[[ -x "${base_verifier}" ]] || die "base verifier not found: ${base_verifier}"

# Forbidden material that must never appear in the provenance artifact. Matches
# the shared verifier's canary so the OCI surface fails closed identically.
readonly forbidden_patterns=(
	'/Users/'
	'/home/'
	'BEGIN [A-Z ]*PRIVATE KEY'
	'[Bb]earer [A-Za-z0-9._-]{8,}'
	'([0-9]{1,3}\.){3}[0-9]{1,3}'
)

print_checks() {
	cat <<CHECKS
component-extension OCI proof checks:
  base: $("${base_verifier}" --list | sed '1d;s/^/  /')
  5. provenance: adapter=oci
  6. provenance: oci_image is digest-pinned (repo@sha256:<64 hex>)
  7. provenance: records eshu_commit, core_version, sdk_protocol, backend, queue_terminal_state
  8. redaction canary: no host paths, private keys, bearer tokens, or raw IPs in provenance
CHECKS
}

if [[ "${list_only}" == true ]]; then
	print_checks
	exit 0
fi

[[ -n "${artifacts_dir}" ]] || die "--artifacts <dir> is required (or use --list)"
[[ -d "${artifacts_dir}" ]] || die "artifacts directory not found: ${artifacts_dir}"

# 1-4. Shared component-extension invariants.
"${base_verifier}" --artifacts "${artifacts_dir}" >/dev/null \
	|| die "base component-extension proof failed"

provenance="${artifacts_dir}/provenance-oci.json"
[[ -f "${provenance}" ]] || die "missing required artifact: ${provenance}"

# 5. Adapter is OCI (proves the digest-pinned container path, not the process
#    adapter).
rg --quiet '"adapter"[[:space:]]*:[[:space:]]*"oci"' "${provenance}" \
	|| die "provenance does not record adapter=oci"

# 6. The launched artifact is digest-pinned (repo@sha256:<64 hex>). A floating
#    tag would mean the worker could run an unverified image.
rg --quiet '"oci_image"[[:space:]]*:[[:space:]]*"[^"]*@sha256:[A-Fa-f0-9]{64}"' "${provenance}" \
	|| die "provenance oci_image is not digest-pinned"

# 7. Required run provenance fields are present.
for field in eshu_commit core_version sdk_protocol backend queue_terminal_state; do
	rg --quiet "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]+\"" "${provenance}" \
		|| die "provenance missing field: ${field}"
done

# 8. Redaction canary over the provenance artifact.
for pattern in "${forbidden_patterns[@]}"; do
	if rg --quiet "${pattern}" "${provenance}"; then
		die "forbidden material matched /${pattern}/ in $(basename "${provenance}")"
	fi
done

printf 'component-extension OCI proof artifacts verified (adapter=oci, digest-pinned)\n'
