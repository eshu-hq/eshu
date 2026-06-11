#!/usr/bin/env bash
set -euo pipefail

# Pin the published reference Scorecard image into the component manifests
# (#2169/#1980). After the publish workflow
# (.github/workflows/publish-scorecard-reference-image.yml) pushes the image to
# the org registry, copy the digest-pinned `repo@sha256:<digest>` ref it prints
# in the run summary and pass it here. This rewrites the artifact image in
# manifest.oci.yaml (the ref the OCI adapter pulls) and the reference-metadata
# image in manifest.yaml, replacing the all-`a` placeholder digests.
#
# Usage (from anywhere):
#   examples/collector-extensions/scorecard/scripts/pin-published-digest.sh \
#     ghcr.io/eshu-hq/examples/scorecard-collector@sha256:<64 hex>

die() { printf 'pin-published-digest: %s\n' "$*" >&2; exit 1; }

ref="${1:-}"
[[ -n "${ref}" ]] || die "usage: pin-published-digest.sh <repo@sha256:<64 hex>>"

# Require a digest-pinned reference; a floating tag would not validate and would
# let the worker run an unverified image.
case "${ref}" in
	*@sha256:*) ;;
	*) die "image reference must be digest-pinned (repo@sha256:<64 hex>): ${ref}" ;;
esac
digest="${ref##*@}"
[[ "${digest}" =~ ^sha256:[A-Fa-f0-9]{64}$ ]] || die "not a sha256 digest: ${digest}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
pkg_dir="$(cd "${script_dir}/.." && pwd)"
oci_manifest="${pkg_dir}/manifest.oci.yaml"
process_manifest="${pkg_dir}/manifest.yaml"

for f in "${oci_manifest}" "${process_manifest}"; do
	[[ -f "${f}" ]] || die "manifest not found: ${f}"
done

# Each manifest declares exactly one artifact `image:` line; rewrite it.
tmp="$(mktemp)"
trap 'rm -f "${tmp}"' EXIT
for f in "${oci_manifest}" "${process_manifest}"; do
	sed "s#^\([[:space:]]*\)image:.*#\1image: ${ref}#" "${f}" >"${tmp}"
	if ! grep -q "image: ${ref}" "${tmp}"; then
		die "no artifact image line rewritten in $(basename "${f}")"
	fi
	mv "${tmp}" "${f}"
	tmp="$(mktemp)"
	printf 'pinned %s -> %s\n' "$(basename "${f}")" "${ref}"
done

cat <<NEXT

Pinned the published digest into both manifests. Verify and gate:
  cd go && go run ./cmd/eshu component verify \\
    ../examples/collector-extensions/scorecard/manifest.oci.yaml \\
    --trust-mode allowlist --allow-id dev.eshu.examples.scorecard --allow-publisher eshu-hq
  git diff --check
NEXT
