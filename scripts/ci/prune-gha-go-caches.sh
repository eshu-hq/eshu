#!/usr/bin/env bash
# Delete superseded Linux-X64 Go Actions caches (`gobuild`, `gobuildrace`,
# `golangcilint`, `go-static`) so daily `gobuild` / `golangcilint` rotation and
# `go.sum` bumps never accumulate past the 10 GB Actions cache quota.
#
# Safety invariants (hardcoded allowlist; refuses positional arguments):
# - Only considers caches on `refs/heads/main`, so PR/branch-scoped caches can
#   never supersede or cause deletion of the shared default-branch cache.
# - Always keeps at least the newest 1 cache entry per logical Go cache family,
#   regardless of age.
# - Only deletes superseded entries once the newest replacement entry in that
#   family is at least `newest_grace_hours` (1h) old AND the superseded entry's
#   `created_at` is older than `PRUNE_GO_MIN_AGE_HOURS` (default 12h).
# - CodeQL, Playwright, Trivy, Node, and macOS caches are never eligible.
#
# Environment:
#   GH_TOKEN                 token with actions: write (workflow GITHUB_TOKEN)
#   PRUNE_REPO               owner/repo (default eshu-hq/eshu)
#   PRUNE_GO_MIN_AGE_HOURS   superseded age threshold in hours (default 12)
#   PRUNE_NOW                epoch seconds to treat as now (tests only)
#   PRUNE_DRY_RUN            1 lists what would be deleted and deletes nothing
set -euo pipefail

readonly keep_newest=1
readonly newest_grace_hours=1
if (($# > 0)); then
	echo "prune-gha-go-caches: takes no arguments; Go cache family allowlist is hardcoded" >&2
	exit 2
fi

repo="${PRUNE_REPO:-eshu-hq/eshu}"
hours="${PRUNE_GO_MIN_AGE_HOURS:-12}"
now="${PRUNE_NOW:-$(date -u +%s)}"
dry_run="${PRUNE_DRY_RUN:-0}"

[[ "${hours}" =~ ^[1-9][0-9]*$ ]] || {
	echo "prune-gha-go-caches: PRUNE_GO_MIN_AGE_HOURS must be a decimal integer >= 1 (no leading zeros), got '${hours}'" >&2
	exit 2
}
[[ "${now}" =~ ^(0|[1-9][0-9]*)$ ]] || {
	echo "prune-gha-go-caches: PRUNE_NOW must be decimal epoch seconds (no leading zeros), got '${now}'" >&2
	exit 2
}
[[ "${dry_run}" == "0" || "${dry_run}" == "1" ]] || {
	echo "prune-gha-go-caches: PRUNE_DRY_RUN must be 0 or 1, got '${dry_run}'" >&2
	exit 2
}

cutoff=$((now - hours * 3600))
grace_cutoff=$((now - newest_grace_hours * 3600))
base="repos/${repo}/actions/caches"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

if ! gh api --paginate "${base}?per_page=100" > "${work}/caches.json" 2> "${work}/err"; then
	cat "${work}/err" >&2
	echo "prune-gha-go-caches: listing ${base} failed" >&2
	exit 1
fi

jq -r -s --argjson cutoff "${cutoff}" --argjson grace "${grace_cutoff}" --argjson keep "${keep_newest}" '
	def go_family:
		if startswith("Linux-X64-gobuild-") then "Linux-X64-gobuild"
		elif startswith("Linux-X64-gobuildrace-") then "Linux-X64-gobuildrace"
		elif startswith("Linux-X64-golangcilint-") then "Linux-X64-golangcilint"
		elif startswith("Linux-X64-go-") then "Linux-X64-go"
		else null
		end;
	[.[].actions_caches[]]
	| map(select(.ref == "refs/heads/main"))
	| map(. + {family: (.key | go_family)})
	| map(select(.family != null))
	| map(.created_ts = ((.created_at // .last_accessed_at) | sub("\\.[0-9]+"; "") | fromdateiso8601))
	| group_by(.family)
	| map(
		sort_by(-.created_ts, -.id)
		| if (length > $keep and .[0].created_ts <= $grace)
		  then .[$keep:] | map(select(.created_ts < $cutoff))
		  else []
		  end
	)
	| add // []
	| .[]
	| "\(.id)\t\(.size_in_bytes // 0)\t\(.key)"
' "${work}/caches.json" > "${work}/targets.tsv"

total="$(jq -s '[.[].actions_caches[]] | length' "${work}/caches.json")"
go_total="$(jq -s '[.[].actions_caches[] | select(.ref == "refs/heads/main") | select((.key | startswith("Linux-X64-gobuild-")) or (.key | startswith("Linux-X64-gobuildrace-")) or (.key | startswith("Linux-X64-golangcilint-")) or (.key | startswith("Linux-X64-go-")))] | length' "${work}/caches.json")"

deleted=0
reclaimed_bytes=0
while IFS=$'\t' read -r id size_bytes key; do
	[[ -n "${id}" ]] || continue
	if [[ "${dry_run}" == "1" ]]; then
		printf 'prune-gha-go-caches: would delete id=%s (%s bytes) key=%s\n' "${id}" "${size_bytes}" "${key}"
	else
		if ! gh api --method DELETE "${base}/${id}" >/dev/null 2> "${work}/del.err"; then
			cat "${work}/del.err" >&2
			echo "prune-gha-go-caches: failed to delete cache id=${id} key=${key}" >&2
			exit 1
		fi
		printf 'prune-gha-go-caches: deleted id=%s (%s bytes) key=%s\n' "${id}" "${size_bytes}" "${key}"
	fi
	deleted=$((deleted + 1))
	reclaimed_bytes=$((reclaimed_bytes + size_bytes))
done < "${work}/targets.tsv"

reclaimed_mb="$(awk -v b="${reclaimed_bytes}" 'BEGIN { printf "%.1f", b / 1000000.0 }')"
verb="deleted"
[[ "${dry_run}" == "1" ]] && verb="would delete"
printf 'prune-gha-go-caches: inspected %s total cache(s) (%s Linux-X64 Go); %s %s superseded cache(s) (~%s MB, keep_newest=%s/family, min_age=%sh, grace=%sh)\n' \
	"${total}" "${go_total}" "${verb}" "${deleted}" "${reclaimed_mb}" "${keep_newest}" "${hours}" "${newest_grace_hours}"
