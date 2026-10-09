#!/usr/bin/env bash
# Resolves the container image ref the post-publish Trivy scan covers (#7699).
# Branch pushes and manual dispatches resolve to the immutable per-commit
# sha-<full sha> tag: under parallel publishing the moving :main tag is racy
# (scans would duplicate and miss), while the per-commit tag exists if and
# only if the triggering run pushed an image. Tag releases keep their version
# ref. When the ref is absent from GHCR — a chart-only run pushed no image —
# the scan skips green instead of failing on a ref that will never exist.
#
# Required env: TRIGGER_HEAD_BRANCH, TRIGGER_HEAD_SHA (either may be empty),
# REPOSITORY, GITHUB_TOKEN (packages read), GITHUB_OUTPUT. Writes ref=<ref>
# (when resolvable) and skip=<true|false> to GITHUB_OUTPUT.
set -euo pipefail

branch="${TRIGGER_HEAD_BRANCH:-}"
sha="${TRIGGER_HEAD_SHA:-}"
repo="${REPOSITORY:?REPOSITORY is required}"
token="${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
out="${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"

ref=""
if [[ "${branch}" =~ ^v[0-9] ]]; then
	ref="ghcr.io/${repo}:${branch#v}"
elif [[ -n "${sha}" ]]; then
	ref="ghcr.io/${repo}:sha-${sha}"
else
	{
		echo "skip=true"
	} >>"${out}"
	echo "image-scan: triggering run has no branch or sha; nothing to scan"
	exit 0
fi

tag="${ref##*:}"
code="$(curl -s -o /dev/null -w '%{http_code}' -u "x-access-token:${token}" \
	-H 'Accept: application/vnd.oci.image.index.v1+json' \
	"https://ghcr.io/v2/${repo}/manifests/${tag}")" || code="000"
case "${code}" in
200)
	{
		echo "ref=${ref}"
		echo "skip=false"
	} >>"${out}"
	echo "image-scan: scanning ${ref}"
	;;
404)
	{
		echo "ref=${ref}"
		echo "skip=true"
	} >>"${out}"
	echo "image-scan: ${ref} is not in GHCR; the triggering run pushed no image, skipping"
	;;
*)
	echo "image-scan: GHCR lookup for ${ref} failed with HTTP ${code}" >&2
	exit 1
	;;
esac
