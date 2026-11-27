#!/usr/bin/env bash
# Delete stale GitHub Default Setup CodeQL Actions caches (`codeql-overlay-*`
# and `codeql-trap-*`) older than the retention window, always keeping the 4
# newest caches per CodeQL cache family.
#
# GitHub Default Setup creates new ~140 MB JS overlay, ~28 MB Python overlay,
# and ~24 MB TRAP cache entries on frequent merges and PRs, which can consume
# >6 GB of the repository's 10 GB Actions cache quota and evict Go/Playwright
# build caches. The allowlist is hardcoded on purpose: Go build/race/mod,
# Playwright, Trivy, and Node caches are never eligible and this script refuses
# any positional arguments.
#
# Environment:
#   GH_TOKEN               token with actions: write (the workflow's GITHUB_TOKEN)
#   PRUNE_REPO             owner/repo (default eshu-hq/eshu)
#   PRUNE_MIN_AGE_HOURS    retention window in hours based on last_accessed_at (default 24)
#   PRUNE_NOW              epoch seconds to treat as now (tests only)
#   PRUNE_DRY_RUN          1 lists what would be deleted and deletes nothing
set -euo pipefail

readonly keep_newest=4
if (($# > 0)); then
	echo "prune-gha-codeql-caches: takes no arguments; prefix allowlist is hardcoded to codeql-overlay- and codeql-trap-" >&2
	exit 2
fi

repo="${PRUNE_REPO:-eshu-hq/eshu}"
hours="${PRUNE_MIN_AGE_HOURS:-24}"
now="${PRUNE_NOW:-$(date -u +%s)}"
dry_run="${PRUNE_DRY_RUN:-0}"

[[ "${hours}" =~ ^[0-9]+$ && "${hours}" -ge 1 ]] || {
	echo "prune-gha-codeql-caches: PRUNE_MIN_AGE_HOURS must be a whole number >= 1, got '${hours}'" >&2
	exit 2
}
[[ "${now}" =~ ^[0-9]+$ ]] || {
	echo "prune-gha-codeql-caches: PRUNE_NOW must be epoch seconds, got '${now}'" >&2
	exit 2
}
[[ "${dry_run}" == "0" || "${dry_run}" == "1" ]] || {
	echo "prune-gha-codeql-caches: PRUNE_DRY_RUN must be 0 or 1, got '${dry_run}'" >&2
	exit 2
}

cutoff=$((now - hours * 3600))
base="repos/${repo}/actions/caches"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

if ! gh api --paginate "${base}?per_page=100" > "${work}/caches.json" 2> "${work}/err"; then
	cat "${work}/err" >&2
	echo "prune-gha-codeql-caches: listing ${base} failed" >&2
	exit 1
fi

# --paginate outputs one object per page (`{"actions_caches": [...]}`).
# Select ONLY codeql-overlay-* and codeql-trap-* entries, group by logical
# family (language for overlay caches, codeql-trap for TRAP caches), sort each
# family newest-first by last_accessed_at, keep the newest keep_newest entries,
# and emit older entries past cutoff as: id<TAB>size_bytes<TAB>key.
jq -r -s --argjson cutoff "${cutoff}" --argjson keep "${keep_newest}" '
	[.[].actions_caches[]?]
	| map(select((.key | startswith("codeql-overlay-")) or (.key | startswith("codeql-trap-"))))
	| map(
		.ts = ((.last_accessed_at // .created_at) | sub("\\.[0-9]+"; "") | fromdateiso8601)
		| .family = (
			if (.key | startswith("codeql-trap-")) then "codeql-trap"
			elif (.key | test("^codeql-overlay-base-database-[0-9]+-[0-9a-f]+-[a-z-]+-")) then
				"codeql-overlay-" + ((.key | capture("^codeql-overlay-base-database-[0-9]+-[0-9a-f]+-(?<lang>[a-z-]+)-").lang))
			else "codeql-overlay-other"
			end
		)
	)
	| group_by(.family)
	| map(sort_by(-.ts) | .[$keep:] | .[] | select(.ts < $cutoff))
	| .[]
	| "\(.id)\t\(.size_in_bytes // 0)\t\(.key)"
' "${work}/caches.json" > "${work}/targets.tsv"

total="$(jq -s '[.[].actions_caches[]?] | length' "${work}/caches.json")"
codeql_total="$(jq -s '[.[].actions_caches[]? | select((.key | startswith("codeql-overlay-")) or (.key | startswith("codeql-trap-")))] | length' "${work}/caches.json")"

deleted=0
reclaimed_bytes=0
while IFS=$'\t' read -r id size_bytes key; do
	[[ -n "${id}" ]] || continue
	if [[ "${dry_run}" == "1" ]]; then
		printf 'prune-gha-codeql-caches: would delete id=%s (%s bytes) key=%s\n' "${id}" "${size_bytes}" "${key}"
	else
		if ! gh api --method DELETE "${base}/${id}" >/dev/null 2> "${work}/del.err"; then
			cat "${work}/del.err" >&2
			echo "prune-gha-codeql-caches: failed to delete cache id=${id} key=${key}" >&2
			exit 1
		fi
		printf 'prune-gha-codeql-caches: deleted id=%s (%s bytes) key=%s\n' "${id}" "${size_bytes}" "${key}"
	fi
	deleted=$((deleted + 1))
	reclaimed_bytes=$((reclaimed_bytes + size_bytes))
done < "${work}/targets.tsv"

reclaimed_mb="$(awk -v b="${reclaimed_bytes}" 'BEGIN { printf "%.1f", b / 1000000.0 }')"
verb="deleted"
[[ "${dry_run}" == "1" ]] && verb="would delete"
printf 'prune-gha-codeql-caches: inspected %s total cache(s) (%s CodeQL); %s %s stale cache(s) (~%s MB, keep_newest=%s/family, min_age=%sh)\n' \
	"${total}" "${codeql_total}" "${verb}" "${deleted}" "${reclaimed_mb}" "${keep_newest}" "${hours}"
