#!/usr/bin/env bash
# check-scanner-freshness.sh - non-blocking freshness signal for the scanner
# digest pins (#9943).
#
# The blocking exactness gate (scripts/verify-scanner-digest-pins.sh) proves
# the pins equal the anchored publish; it deliberately does not check whether
# the anchor lags a newer satellite publish. This script is that missing
# signal: it compares the manifest anchor to the latest *successful* run of
# the satellite `release` workflow and reports FRESH or STALE. A successful
# run is the reference because only it publishes images; a newer satellite
# commit with no successful release yet is correctly still FRESH, and GitHub
# releases or tags are never the reference (the satellite publishes none).
#
# Lag bound: a security publish needs an expedited eshu re-pin PR (within one
# business day, per the satellite SECURITY.md "Older digests: No" row); a
# routine publish rides the next reviewed re-pin window. At 0 replicas a stale
# pin runs nothing, so the signal informs the next activation rather than
# paging anyone. Standing activation requires this signal green or a dated
# manual anchor check (recorded on #8878).
#
# Exit codes: 0 FRESH; 1 STALE (the flag a schedule or reviewer acts on);
# 2 fail-closed on bad usage, an unreadable manifest, a missing or malformed
# anchor or publish-repo line, zero successful releases, a transport failure,
# or an unparseable release answer. Unknown is never fresh and never stale.
#
# Usage:
#   scripts/check-scanner-freshness.sh [--manifest PATH]
#
# Environment:
#   SCANNER_FRESHNESS_REPO      Satellite repository holding the images
#                               (default eshu-hq/eshu-scanner-worker).
#   SCANNER_FRESHNESS_WORKFLOW  Release workflow file or ID
#                               (default release.yml).
#   GH_BIN                      gh binary (default gh).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${repo_root}/go/internal/collector/scannercomponent/manifest.go"
publish_repo="${SCANNER_FRESHNESS_REPO:-eshu-hq/eshu-scanner-worker}"
workflow="${SCANNER_FRESHNESS_WORKFLOW:-release.yml}"
gh_bin="${GH_BIN:-gh}"

die() {
	printf 'check-scanner-freshness: FAIL-CLOSED: %s\n' "$*" >&2
	exit 2
}

while (($# > 0)); do
	case "$1" in
		--manifest)
			(($# >= 2)) || die "--manifest needs a path"
			manifest="$2"
			shift 2
			;;
		*)
			printf 'check-scanner-freshness: unknown argument %s\n' "$1" >&2
			exit 2
			;;
	esac
done

[[ -r "${manifest}" ]] || die "cannot read manifest: ${manifest}"
command -v "${gh_bin}" >/dev/null 2>&1 || die "required tool not found: ${gh_bin}"
command -v python3 >/dev/null 2>&1 || die "required tool not found: python3"

anchor_count="$(grep -c '^// Scanner-Publish-SHA: ' "${manifest}" || true)"
[[ "${anchor_count}" == 1 ]] || die "want exactly one '// Scanner-Publish-SHA: <40-hex>' line in ${manifest}, found ${anchor_count}"
anchor="$(sed -n 's|^// Scanner-Publish-SHA: \([0-9a-f]*\)$|\1|p' "${manifest}")"
[[ "${anchor}" =~ ^[0-9a-f]{40}$ ]] || die "malformed anchor line in ${manifest}; want '// Scanner-Publish-SHA: <40-hex>'"

repo_count="$(grep -c '^// Scanner-Publish-Repo: ' "${manifest}" || true)"
[[ "${repo_count}" == 1 ]] || die "want exactly one '// Scanner-Publish-Repo: <owner/name>' line in ${manifest}, found ${repo_count}"
manifest_repo="$(sed -n 's|^// Scanner-Publish-Repo: \([A-Za-z0-9_.-]*/[A-Za-z0-9_.-]*\)$|\1|p' "${manifest}")"
[[ -n "${manifest_repo}" ]] || die "malformed publish-repo line in ${manifest}; want '// Scanner-Publish-Repo: <owner/name>'"

release_json="$("${gh_bin}" api "repos/${publish_repo}/actions/workflows/${workflow}/runs?status=success&per_page=1" 2>/dev/null)" \
	|| die "cannot list successful ${workflow} runs in ${publish_repo}; network or auth failure"

read -r release_head release_id release_event release_date <<<"$(
	python3 -c 'import json,sys
runs=json.load(sys.stdin).get("workflow_runs",[])
if not runs:
    sys.exit(1)
run=runs[0]
print(run.get("head_sha",""),run.get("id",""),run.get("event",""),run.get("created_at",""))' \
		<<<"${release_json}" 2>/dev/null
)" || die "no successful ${workflow} run to compare against, or the release answer does not parse"
[[ "${release_head}" =~ ^[0-9a-f]{40}$ ]] || die "the latest successful release head is not a 40-hex commit: ${release_head}"

if [[ "${anchor}" == "${release_head}" ]]; then
	printf 'FRESH anchor %s equals the latest successful satellite release %s (run %s, %s, %s)\n' \
		"${anchor:0:7}" "${release_head:0:7}" "${release_id}" "${release_event}" "${release_date}"
	exit 0
fi

printf 'STALE anchor %s lags the latest successful satellite release %s (run %s, %s, %s)\n' \
	"${anchor:0:7}" "${release_head:0:7}" "${release_id}" "${release_event}" "${release_date}"
exit 1
