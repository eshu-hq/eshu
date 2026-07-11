#!/usr/bin/env bash
set -euo pipefail

# Live proof for the Scorecard component-extension OCI adapter (#1980). It builds
# the standalone reference image (examples/collector-extensions/scorecard/
# Dockerfile.oci), pushes it to a registry to obtain an immutable digest, then
# launches the digest-pinned artifact through the exact isolation contract the
# host extensionhost.OCIRunner uses and asserts the collector emits the three
# dev.eshu.examples.scorecard.* fact families over the SDK stdio boundary.
#
# This proves the parts a process-backed run cannot: a published, digest-pinned
# artifact; real image pull and digest resolution; and bounded stdin/stdout
# execution with no network, a read-only rootfs, dropped capabilities, and a
# non-root user — i.e. no Eshu handles reach the extension.
#
# Requires a working container engine (docker or podman) and a reachable
# registry. The registry defaults to a local registry:2 the script can start.
#
# Usage (from repo root):
#   scripts/verify-oci-scorecard-adapter.sh [--registry host:port] [--keep] [--list]

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
runtime="${ESHU_OCI_RUNTIME:-docker}"
registry="${ESHU_OCI_REGISTRY:-localhost:5000}"
image_repo="eshu-examples/scorecard-collector"
list_only=false
keep=false

die() { printf 'verify-oci-scorecard-adapter: %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
	case "$1" in
		--list) list_only=true; shift ;;
		--registry) registry="${2:?}"; shift 2 ;;
		--keep) keep=true; shift ;;
		-h|--help) sed -n '3,21p' "$0"; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v rg >/dev/null 2>&1 || die "rg is required"

# The bounded SDK request the host sends on stdin. config.source.input is the
# IN-IMAGE fixture path baked by Dockerfile.oci — never a host path — so the
# read-only, network-less container resolves it without any mount or leak.
# The body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1
# writes the entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on any body over that size (#5074).
read -r -d '' sdk_request <"${repo_root}/scripts/lib/verify-oci-scorecard-adapter-sdk-request.json" || true

fact_families=(
	"dev.eshu.examples.scorecard.snapshot"
	"dev.eshu.examples.scorecard.check"
	"dev.eshu.examples.scorecard.warning"
)
forbidden_patterns=('/Users/' '/home/' 'BEGIN [A-Z ]*PRIVATE KEY' '[Bb]earer [A-Za-z0-9._-]{8,}' '([0-9]{1,3}\.){3}[0-9]{1,3}')

print_checks() {
	cat <<CHECKS
oci scorecard adapter proof checks:
  1. build: reference image builds from Dockerfile.oci (pure-Go, distroless nonroot)
  2. publish: image pushes to ${registry} and resolves an immutable repo@sha256 digest
  3. isolation: digest-pinned artifact runs with --network none --read-only --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges
  4. facts: stdout carries the families ${fact_families[*]}
  5. redaction canary: stdout has no host path, private key, bearer token, or raw IP
CHECKS
}

if [[ "${list_only}" == true ]]; then print_checks; exit 0; fi

command -v "${runtime}" >/dev/null 2>&1 || die "container runtime '${runtime}' not found"
"${runtime}" version >/dev/null 2>&1 || die "container engine '${runtime}' is not reachable (is the daemon running?)"

# Bring up a throwaway local registry if the target is localhost and not already serving.
registry_container=""
if [[ "${registry}" == localhost:* || "${registry}" == 127.0.0.1:* ]]; then
	if ! "${runtime}" exec eshu-oci-registry true >/dev/null 2>&1; then
		port="${registry##*:}"
		registry_container="eshu-oci-registry"
		"${runtime}" rm -f "${registry_container}" >/dev/null 2>&1 || true
		"${runtime}" run -d --name "${registry_container}" -p "${port}:5000" registry:2 >/dev/null \
			|| die "failed to start local registry on ${registry}"
		# Give the registry a moment to accept pushes.
		for _ in 1 2 3 4 5; do "${runtime}" exec "${registry_container}" true >/dev/null 2>&1 && break; sleep 1; done
	fi
fi

cleanup() {
	[[ "${keep}" == true ]] && return 0
	[[ -n "${registry_container}" ]] && "${runtime}" rm -f "${registry_container}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

tag="${registry}/${image_repo}:proof"

# 1. Build the reference image.
"${runtime}" build -t "${tag}" -f examples/collector-extensions/scorecard/Dockerfile.oci "${repo_root}" \
	|| die "reference image build failed"

# 2. Push and resolve the immutable digest.
"${runtime}" push "${tag}" >/dev/null || die "push to ${registry} failed"
digest="$("${runtime}" inspect --format '{{ index .RepoDigests 0 }}' "${tag}" 2>/dev/null \
	| rg -o "@sha256:[A-Fa-f0-9]{64}$" || true)"
[[ -n "${digest}" ]] || die "could not resolve a repo@sha256 digest after push"
pinned="${registry}/${image_repo}${digest}"
printf 'resolved digest-pinned artifact: %s\n' "${pinned}"

# 3. Run the digest-pinned artifact through the exact OCIRunner isolation flags.
stdout_file="$(mktemp)"
trap 'rm -f "${stdout_file}"; cleanup' EXIT
if ! printf '%s' "${sdk_request}" | "${runtime}" run --rm --interactive \
	--network none --read-only \
	--user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
	"${pinned}" >"${stdout_file}" 2>/dev/null; then
	die "digest-pinned artifact run failed under the isolation contract"
fi

# 4. Fact families.
for family in "${fact_families[@]}"; do
	rg --fixed-strings --quiet "\"${family}\"" "${stdout_file}" \
		|| die "stdout missing fact family: ${family}"
done

# 5. Redaction canary on the emitted result.
for pattern in "${forbidden_patterns[@]}"; do
	if rg --quiet "${pattern}" "${stdout_file}"; then
		die "forbidden material matched /${pattern}/ in OCI adapter stdout"
	fi
done

printf 'oci scorecard adapter proof verified (digest-pinned, isolated, %d fact families)\n' "${#fact_families[@]}"
