#!/usr/bin/env bash
set -euo pipefail

# Publish the reference Scorecard collector OCI image to the org container
# registry (#2169/#1980) and print the immutable digest to pin into the
# manifests. This is the maintainer-run publish path; it needs a registry login
# with package-write scope and does not embed any credential.
#
# Prerequisite — log in to the registry with a token that can write packages:
#   echo "$CR_PAT" | docker login ghcr.io -u <username> --password-stdin
#
# Usage (from anywhere in the repo):
#   examples/collector-extensions/scorecard/scripts/publish-reference-image.sh
#
# Overrides (defaults match the manifest's declared canonical location):
#   SCORECARD_REGISTRY   default ghcr.io
#   SCORECARD_OWNER      default eshu-hq        (the public OSS org)
#   SCORECARD_VERSION    default 0.1.0          (manifest.oci.yaml version)
#   SCORECARD_PLATFORMS  default linux/amd64,linux/arm64

die() { printf 'publish-reference-image: %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || die "docker is required"
docker buildx version >/dev/null 2>&1 || die "docker buildx is required for a multi-arch push"

registry="${SCORECARD_REGISTRY:-ghcr.io}"
owner="${SCORECARD_OWNER:-eshu-hq}"
version="${SCORECARD_VERSION:-0.1.0}"
platforms="${SCORECARD_PLATFORMS:-linux/amd64,linux/arm64}"
repo="${registry}/${owner}/examples/scorecard-collector"

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/../../../.." && pwd))"
dockerfile="${repo_root}/examples/collector-extensions/scorecard/Dockerfile.oci"
[[ -f "${dockerfile}" ]] || die "missing ${dockerfile}"

printf 'publishing %s:{%s,latest} for %s\n' "${repo}" "${version}" "${platforms}"
docker buildx build --push \
	--platform "${platforms}" \
	-f "${dockerfile}" \
	-t "${repo}:${version}" \
	-t "${repo}:latest" \
	"${repo_root}"

# Resolve the immutable manifest-list digest of the just-pushed version tag.
digest="$(docker buildx imagetools inspect "${repo}:${version}" \
	--format '{{.Manifest.Digest}}' 2>/dev/null)" \
	|| die "could not resolve published digest for ${repo}:${version}"
[[ "${digest}" =~ ^sha256:[A-Fa-f0-9]{64}$ ]] || die "unexpected digest: ${digest}"

ref="${repo}@${digest}"
printf '\npublished %s\n' "${ref}"
printf 'pin it into the manifests:\n  %s/examples/collector-extensions/scorecard/scripts/pin-published-digest.sh %s\n' \
	"${repo_root}" "${ref}"
