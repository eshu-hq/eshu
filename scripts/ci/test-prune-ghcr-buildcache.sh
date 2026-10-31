#!/usr/bin/env bash
# Hermetic RED/GREEN suite for scripts/ci/prune-ghcr-buildcache.sh (#9632):
# only untagged versions older than the window are deleted, a tagged version
# never is, the 4 newest untagged versions always survive, the script can
# only ever name the eshu-buildcache package, a missing package is a clean
# no-op only when the caller says so, and any API failure fails.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir "${work}/bin"
cp "${root}/scripts/ci/fixtures/gh-packages.sh" "${work}/bin/gh"
chmod +x "${work}/bin/gh"
export PATH="${work}/bin:${PATH}" MOCK_GH_PAGES="${work}/pages.json" MOCK_GH_CALLS="${work}/calls"
# now = 2026-10-30T00:00:00Z; the 7-day cutoff is 2026-10-23T00:00:00Z.
export PRUNE_NOW=1793318400
tagged='{"metadata":{"container":{"tags":["runtime"]}}'
untagged='{"metadata":{"container":{"tags":[]}}'
# Busy package: 2 tagged versions (ids 1, 5), 5 untagged inside the window
# (ids 3, 6, 7, 8, 10), 3 untagged past it (ids 2, 4, 9). The 4 newest untagged
# are 8, 7, 6, 10. Id 3 is beyond them but inside the window, so only 2, 4
# and 9 go.
busy_pages() {
	{
		printf '[%s,"id":1,"updated_at":"2026-10-01T00:00:00Z"},' "${tagged}"
		printf '%s,"id":2,"updated_at":"2026-10-22T23:59:59Z"},' "${untagged}"
		printf '%s,"id":3,"updated_at":"2026-10-23T00:00:01.5Z"},' "${untagged}"
		printf '%s,"id":6,"updated_at":"2026-10-25T00:00:00Z"},' "${untagged}"
		printf '%s,"id":10,"updated_at":"2026-10-24T00:00:00Z"}]\n' "${untagged}"
		printf '[%s,"id":4,"updated_at":"2026-09-01T00:00:00.123Z"},' "${untagged}"
		printf '%s,"id":5,"updated_at":"2026-10-29T00:00:00Z"},' "${tagged}"
		printf '%s,"id":7,"updated_at":"2026-10-27T00:00:00Z"},' "${untagged}"
		printf '%s,"id":8,"updated_at":"2026-10-29T12:00:00Z"},' "${untagged}"
		printf '%s,"id":9,"updated_at":"2026-08-01T00:00:00Z"}]\n' "${untagged}"
	} > "${MOCK_GH_PAGES}"
}
# Idle package: no main push for a month, so all 5 untagged versions are past
# the window. The 4 newest (11-14) survive, so a tag a concurrent run may still
# import is never deleted; only id 15 goes.
idle_pages() {
	{
		printf '[%s,"id":11,"updated_at":"2026-09-05T00:00:00Z"},' "${untagged}"
		printf '%s,"id":12,"updated_at":"2026-09-04T00:00:00Z"},' "${untagged}"
		printf '%s,"id":15,"updated_at":"2026-09-01T00:00:00Z"}]\n' "${untagged}"
		printf '[%s,"id":13,"updated_at":"2026-09-03T00:00:00Z"},' "${untagged}"
		printf '%s,"id":14,"updated_at":"2026-09-02T00:00:00Z"}]\n' "${untagged}"
	} > "${MOCK_GH_PAGES}"
}
prune() { bash "${root}/scripts/ci/prune-ghcr-buildcache.sh" "$@" > "${work}/out" 2>&1; }
deletes() { sed -n 's|.*-X DELETE .*/versions/\([0-9][0-9]*\)$|\1|p' "${MOCK_GH_CALLS}" | sort | tr '\n' ' '; }
fail() { echo "test-prune-ghcr-buildcache: FAIL: $*" >&2; cat "${work}/out" >&2; exit 1; }

busy_pages
: > "${MOCK_GH_CALLS}"
prune || fail "clean prune exited non-zero"
[[ "$(deletes)" == "2 4 9 " ]] || fail "want deletes '2 4 9 ', got '$(deletes)'"
[[ "$(head -1 "${MOCK_GH_CALLS}")" == "api --paginate orgs/eshu-hq/packages/container/eshu-buildcache/versions?per_page=100" ]] || fail "must list the eshu-buildcache package only"
[[ "$(cat "${work}/out")" == *"eshu-buildcache: 10 version(s), 8 untagged, 3 deleted (older than 7 day(s), beyond the 4 newest)"* ]] || fail "summary line"
echo "PASS: deletes only untagged versions older than 7 days beyond the 4 newest, across pages"

idle_pages
: > "${MOCK_GH_CALLS}"
prune || fail "idle prune exited non-zero"
[[ "$(deletes)" == "15 " ]] || fail "idle package: want deletes '15 ', got '$(deletes)'"
echo "PASS: the 4 newest untagged versions survive even when all are past the window"

busy_pages
: > "${MOCK_GH_CALLS}"
PRUNE_PACKAGE=eshu prune || fail "PRUNE_PACKAGE must be ignored, not fatal"
[[ "$(head -1 "${MOCK_GH_CALLS}")" == "api --paginate orgs/eshu-hq/packages/container/eshu-buildcache/versions?per_page=100" ]] || fail "PRUNE_PACKAGE redirected the listing"
[[ "$(deletes)" == "2 4 9 " ]] || fail "PRUNE_PACKAGE changed the deletes: '$(deletes)'"
for other in eshu scanner-image eshu-buildcache; do
	: > "${MOCK_GH_CALLS}"
	if prune "${other}"; then fail "a package argument '${other}' must be refused"; fi
	[[ ! -s "${MOCK_GH_CALLS}" ]] || fail "a package argument '${other}' reached the API: $(cat "${MOCK_GH_CALLS}")"
done
echo "PASS: no input can name another package; eshu and scanner-* are never listed or deleted"

: > "${MOCK_GH_CALLS}"
PRUNE_DRY_RUN=1 prune || fail "dry run exited non-zero"
[[ -z "$(deletes)" ]] || fail "dry run deleted '$(deletes)'"
echo "PASS: dry run deletes nothing"

: > "${MOCK_GH_CALLS}"
if MOCK_GH_LIST=404 prune; then fail "a 404 must fail: a token that cannot see the package gets the same answer as a missing one"; fi
[[ "$(cat "${work}/out")" == *"HTTP 404"* && "$(cat "${work}/out")" == *"cannot see"* ]] || fail "the 404 failure must name both causes"
[[ -z "$(deletes)" ]] || fail "a 404 deleted '$(deletes)'"
MOCK_GH_LIST=404 PRUNE_ALLOW_MISSING=1 prune || fail "PRUNE_ALLOW_MISSING=1 must make a 404 a clean no-op"
[[ "$(cat "${work}/out")" == *"does not exist yet"* ]] || fail "the allowed 404 must say the package does not exist yet"
[[ -z "$(deletes)" ]] || fail "an allowed 404 deleted '$(deletes)'"
echo "PASS: a 404 fails loudly unless PRUNE_ALLOW_MISSING=1"

: > "${MOCK_GH_CALLS}"
if MOCK_GH_LIST=500 prune; then fail "listing failure must fail"; fi
[[ -z "$(deletes)" ]] || fail "listing failure deleted '$(deletes)'"
: > "${MOCK_GH_CALLS}"
if MOCK_GH_DELETE=fail prune; then fail "delete failure must fail"; fi
if PRUNE_MIN_AGE_DAYS=0 prune; then fail "a zero-day window must be refused"; fi
echo "PASS: listing, delete, and window errors fail"
echo "test-prune-ghcr-buildcache: all cases passed"
