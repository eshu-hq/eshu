#!/usr/bin/env bash
# Pins the #7699 publish-concurrency contract on the workflow files: per-sha
# push groups, no moving tags in the build list with a pinned long sha format,
# the guarded promotion job and its digest plumbing, the Helm duplicate-push
# tolerance, and the per-sha Trivy resolver with its skip-green path. Sibling
# to test-verify-docker-publish-pr-platforms.sh, which owns the PR platform and
# publication-guard pins; both run in the static-contract-gates matrix.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

publish=".github/workflows/docker-publish.yml"
scan=".github/workflows/security-scan.yml"
promote="scripts/ci/promote-moving-tags.sh"
resolver="scripts/ci/resolve-image-scan-ref.sh"

require_pattern() {
	local file="$1"
	local pattern="$2"
	local message="$3"

	if ! rg -F -q -- "$pattern" "$file"; then
		printf '%s\n' "$message" >&2
		exit 1
	fi
}

forbid_pattern() {
	local file="$1"
	local pattern="$2"
	local message="$3"

	if rg -F -q -- "$pattern" "$file"; then
		printf '%s\n' "$message" >&2
		exit 1
	fi
}

# 1. Every main commit gets its own lossless run; tags stay on the ref.
require_pattern \
	"$publish" \
	"group: \${{ github.workflow }}-\${{ github.event_name == 'push' && github.ref == 'refs/heads/main' && github.sha || github.ref }}" \
	'docker publish workflow must key main-push concurrency on the commit sha'

# 2. The build pushes immutable tags only: no branch tag, no latest tag, and
# the sha format pinned long (the scan resolver and the promotion sanity check
# both address sha-<full sha>).
require_pattern \
	"$publish" \
	'type=sha,prefix=sha-,format=long' \
	'docker publish workflow must pin the per-commit tag to the long sha format'
forbid_pattern \
	"$publish" \
	'type=ref,event=branch' \
	'docker publish build list must not push the moving branch tag (promotion owns it)'
forbid_pattern \
	"$publish" \
	'value=latest' \
	'docker publish build list must not push the moving latest tag (promotion owns it)'

# 3. The promotion job: digest plumbing, push/dispatch-only lane, no
# job-level mutex (a mutex group would drop intermediate promotions).
require_pattern \
	"$publish" \
	'digest: ${{ steps.build.outputs.digest }}' \
	'docker publish build job must expose the pushed digest to promotion'
require_pattern \
	"$publish" \
	'promote-moving-tags:' \
	'docker publish workflow must carry the moving-tag promotion job'
require_pattern \
	"$publish" \
	'bash scripts/ci/promote-moving-tags.sh' \
	'docker publish promotion job must run the ancestry-guarded promotion script'
require_pattern \
	"$publish" \
	"needs.build-and-push-image.result == 'success'" \
	'docker publish promotion must require a successful image build'
if [[ "$(rg -c '^concurrency:' "$publish")" != "1" ]]; then
	printf '%s\n' 'docker publish workflow must have exactly one (top-level) concurrency group; the promotion job must not carry a mutex' >&2
	exit 1
fi
require_pattern \
	"$promote" \
	'merge-base --is-ancestor' \
	'promotion script must guard the moving tags on commit ancestry'
require_pattern \
	"$promote" \
	'"${image}:main" -t "${image}:latest"' \
	'promotion script must move main and latest together in one command'

# 4. The Helm push tolerates a same-version duplicate only when the bytes are
# identical; anything else stays red.
require_pattern \
	"$publish" \
	'already published with identical bytes' \
	'docker publish workflow must tolerate only byte-identical Helm duplicate pushes'

# 5. Trivy scans the per-commit sha tag, never the racy moving tag, and skips
# green when the triggering run pushed no image (chart-only).
require_pattern \
	"$scan" \
	'bash scripts/ci/resolve-image-scan-ref.sh' \
	'security scan workflow must resolve the image ref through the per-sha resolver'
forbid_pattern \
	"$scan" \
	'sha-${sha::12}' \
	'security scan workflow must not resolve the dead 12-char sha arm'
forbid_pattern \
	"$scan" \
	'ref="ghcr.io/${REPOSITORY}:${branch}"' \
	'security scan workflow must not scan the moving branch tag'
if [[ "$(rg -c "steps.image.outputs.skip != 'true'" "$scan")" -lt "2" ]]; then
	printf '%s\n' 'security scan workflow must gate both the image scan and its SARIF upload on the resolver skip flag' >&2
	exit 1
fi
require_pattern \
	"$scan" \
	"steps.image.outcome == 'success'" \
	'security scan SARIF upload must require a successful resolver run (no upload attempt on resolver failure)'
require_pattern \
	"$resolver" \
	'ref="ghcr.io/${repo}:sha-${sha}"' \
	'scan resolver must address the full-sha per-commit tag'
require_pattern \
	"$resolver" \
	'echo "skip=true"' \
	'scan resolver must carry the skip-green path'

printf 'docker publish concurrency shape guards passed\n'
