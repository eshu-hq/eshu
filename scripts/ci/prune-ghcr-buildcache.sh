#!/usr/bin/env bash
# Delete untagged versions of the eshu-buildcache GHCR package that are older
# than the retention window, always keeping the 4 newest untagged versions
# (#9632).
#
# docker-publish.yml exports its BuildKit cache to one fixed tag per target in
# ghcr.io/eshu-hq/eshu-buildcache. Each main push moves the tag, and GHCR keeps
# the version it left untagged until someone deletes it. The package name is
# fixed on purpose: the image packages (eshu, scanner-*) hold untagged
# per-platform child manifests of every multi-arch image, and deleting those
# breaks the image, so this script never takes a package argument and refuses
# one. The 4 newest untagged versions survive whatever their age: a tag moved
# by a concurrent main push leaves the version another run may still be
# importing untagged, and a package nobody pushed to for a month must not be
# emptied.
#
# Environment:
#   GH_TOKEN               token with packages: write (the workflow's GITHUB_TOKEN)
#   PRUNE_ORG              owning organization (default eshu-hq)
#   PRUNE_MIN_AGE_DAYS     retention window in days (default 7)
#   PRUNE_NOW              epoch seconds to treat as now (tests only)
#   PRUNE_DRY_RUN          1 lists what would be deleted and deletes nothing
#   PRUNE_ALLOW_MISSING    1 treats a 404 as "package not created yet" (default: a 404 fails)
set -euo pipefail

readonly package="eshu-buildcache"
readonly keep_newest=4
if (($# > 0)); then
	echo "prune: takes no arguments; the package is fixed to ${package}" >&2
	exit 2
fi
org="${PRUNE_ORG:-eshu-hq}"
days="${PRUNE_MIN_AGE_DAYS:-7}"
now="${PRUNE_NOW:-$(date -u +%s)}"
dry_run="${PRUNE_DRY_RUN:-0}"
allow_missing="${PRUNE_ALLOW_MISSING:-0}"
[[ "${days}" =~ ^[0-9]+$ && "${days}" -ge 1 ]] || { echo "prune: PRUNE_MIN_AGE_DAYS must be a whole number >= 1, got '${days}'" >&2; exit 2; }
cutoff=$((now - days * 86400))
base="orgs/${org}/packages/container/${package}/versions"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
if ! gh api --paginate "${base}?per_page=100" > "${work}/versions.json" 2> "${work}/err"; then
	if [[ "$(cat "${work}/err")" == *"HTTP 404"* ]]; then
		# GitHub answers 404 both for a package that does not exist and for a
		# private package the token cannot see, so a bare 404 must not look
		# like success: a daily run that never sees the package would be green
		# and delete nothing for ever. A caller that knows the package may be
		# missing (the first run before any main push) opts in explicitly.
		if [[ "${allow_missing}" == 1 ]]; then
			echo "prune: package ${org}/${package} does not exist yet (PRUNE_ALLOW_MISSING=1); nothing to prune"
			exit 0
		fi
		echo "prune: HTTP 404 listing ${org}/${package}: the package does not exist yet, or the token cannot see it (a private package that is not linked to this repository). Set PRUNE_ALLOW_MISSING=1 only if no main push has written the cache." >&2
		exit 1
	fi
	cat "${work}/err" >&2
	echo "prune: listing ${base} failed" >&2
	exit 1
fi

# --paginate prints one JSON array per page; slurp and join them. A version
# with any tag is never selected, whatever its age. Untagged versions are
# ranked newest first, the first keep_newest are kept, and of the rest only
# those past the cutoff are selected.
jq -r -s --argjson cutoff "${cutoff}" --argjson keep "${keep_newest}" '
	add // []
	| map(select((.metadata.container.tags | length) == 0)
		| .ts = (.updated_at | sub("\\.[0-9]+"; "") | fromdateiso8601))
	| sort_by(-.ts) | .[$keep:] | .[]
	| select(.ts < $cutoff)
	| .id' "${work}/versions.json" > "${work}/ids"
total="$(jq -s 'add // [] | length' "${work}/versions.json")"
untagged_total="$(jq -s 'add // [] | map(select((.metadata.container.tags | length) == 0)) | length' "${work}/versions.json")"

deleted=0
while read -r id; do
	[[ -n "${id}" ]] || continue
	if [[ "${dry_run}" == 1 ]]; then
		echo "prune: would delete ${package} version ${id}"
		continue
	fi
	gh api -X DELETE "${base}/${id}" > /dev/null
	deleted=$((deleted + 1))
done < "${work}/ids"

echo "prune: ${package}: ${total} version(s), ${untagged_total} untagged, ${deleted} deleted (older than ${days} day(s), beyond the ${keep_newest} newest)"
