#!/usr/bin/env bash
# Hermetic self-test for scripts/ci/prune-gha-codeql-caches.sh (zero network,
# mocked gh binary, zero rg dependency, zero large heredocs).
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
script="${repo_root}/scripts/ci/prune-gha-codeql-caches.sh"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

pass=0
fail=0
ok() { pass=$((pass + 1)); printf 'ok - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s\n' "$1" >&2; }

# Mock gh CLI controlled by FAKE_GH_FIXTURE and FAKE_GH_FAIL_MODE (<512B heredoc).
mkdir -p "${work}/bin"
cat > "${work}/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == "api" ]] || exit 99
shift
if [[ "${1:-}" == "--paginate" ]]; then
	[[ "${FAKE_GH_FAIL_MODE:-}" != "list" ]] || { echo "HTTP 403" >&2; exit 1; }
	cat "${FAKE_GH_FIXTURE}"
	exit 0
fi
if [[ "${1:-}" == "--method" && "${2:-}" == "DELETE" ]]; then
	[[ "${FAKE_GH_FAIL_MODE:-}" != "delete" ]] || { echo "HTTP 500" >&2; exit 1; }
	printf '%s\n' "${3:-}" >> "${FAKE_GH_DELETES}"
	exit 0
fi
exit 99
EOF
chmod +x "${work}/bin/gh"

# Fixed "now": 2026-11-27T00:00:00Z = 1795737600
now_epoch=1795737600

# Two-page paginated fixture:
# - 7 javascript overlay caches:
#     5 inside the 24h retention window (101..105 on 2026-11-26; 105 is rank 5 and
#       uses created_at fallback with last_accessed_at: null -> MUST survive cutoff guard)
#     2 stale >24h old (106, 107 on 2026-11-25 -> expect deleted)
# - 5 trap caches (all >24h old -> newest 4 kept, oldest 1 deleted: id 205)
# - 3 python overlay caches (all >24h old -> <=4 so 0 deleted)
# - 3 non-CodeQL caches (30 days old -> MUST NEVER be deleted: ids 301, 302, 303)
printf '%s\n' \
	'{"actions_caches":[' \
	'  {"id":101,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a1","size_in_bytes":140000000,"last_accessed_at":"2026-11-26T23:00:00.000Z"},' \
	'  {"id":102,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a2","size_in_bytes":140000000,"last_accessed_at":"2026-11-26T20:00:00.000Z"},' \
	'  {"id":103,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a3","size_in_bytes":140000000,"last_accessed_at":"2026-11-26T18:00:00.000Z"},' \
	'  {"id":104,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a4","size_in_bytes":140000000,"last_accessed_at":"2026-11-26T12:00:00.000Z"},' \
	'  {"id":105,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a5","size_in_bytes":140000000,"last_accessed_at":null,"created_at":"2026-11-26T06:00:00.000Z"},' \
	'  {"id":106,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.5-a6","size_in_bytes":140000000,"last_accessed_at":"2026-11-25T09:00:00.000Z"},' \
	'  {"id":107,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.5-a7","size_in_bytes":140000000,"last_accessed_at":"2026-11-25T08:00:00.000Z"},' \
	'  {"id":301,"key":"Linux-X64-gobuild-deadbeef","size_in_bytes":1600000000,"last_accessed_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":302,"key":"Linux-X64-gobuildrace-deadbeef","size_in_bytes":530000000,"last_accessed_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":303,"key":"playwright-chromium-Linux-deadbeef","size_in_bytes":270000000,"last_accessed_at":"2026-10-20T00:00:00.000Z"}' \
	']}' \
	'{"actions_caches":[' \
	'  {"id":201,"key":"codeql-trap-1-a","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T12:00:00.000Z"},' \
	'  {"id":202,"key":"codeql-trap-1-b","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T11:00:00.000Z"},' \
	'  {"id":203,"key":"codeql-trap-1-c","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T10:00:00.000Z"},' \
	'  {"id":204,"key":"codeql-trap-1-d","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T09:00:00.000Z"},' \
	'  {"id":205,"key":"codeql-trap-1-e","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T08:00:00.000Z"},' \
	'  {"id":251,"key":"codeql-overlay-base-database-1-abc123-python-2.27.5-p1","size_in_bytes":27000000,"last_accessed_at":"2026-11-24T01:00:00.000Z"},' \
	'  {"id":252,"key":"codeql-overlay-base-database-1-abc123-python-2.27.5-p2","size_in_bytes":27000000,"last_accessed_at":"2026-11-24T02:00:00.000Z"},' \
	'  {"id":253,"key":"codeql-overlay-base-database-1-abc123-python-2.27.5-p3","size_in_bytes":27000000,"last_accessed_at":"2026-11-24T03:00:00.000Z"}' \
	']}' > "${work}/fixture.json"

# 1. Refuses positional arguments
set +e
PATH="${work}/bin:${PATH}" bash "${script}" extra-arg >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 2 ]]; then ok "rejects positional arguments with exit 2"; else bad "expected exit 2 on arg, got ${rc}"; fi

# 2. Rejects invalid PRUNE_MIN_AGE_HOURS (including leading-zero octal traps)
for bad_h in 0 -5 abc 08 010; do
	set +e
	PATH="${work}/bin:${PATH}" PRUNE_MIN_AGE_HOURS="${bad_h}" bash "${script}" >/dev/null 2>&1
	rc=$?
	set -e
	if [[ "${rc}" -eq 2 ]]; then ok "rejects PRUNE_MIN_AGE_HOURS=${bad_h}"; else bad "expected exit 2 for hours=${bad_h}, got ${rc}"; fi
done

# 3. Dry-run lists deletions and issues zero DELETE calls
: > "${work}/deletes-dry.log"
out="$(PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-dry.log" PRUNE_NOW="${now_epoch}" PRUNE_DRY_RUN=1 bash "${script}")"
if [[ ! -s "${work}/deletes-dry.log" ]] && [[ "${out}" == *"would delete 3 stale cache(s)"* ]]; then
	ok "dry-run issues zero DELETEs and reports 3 stale caches"
else
	bad "dry-run failed: out=${out}"
fi

# 4. Live run deletes exact 3 stale CodeQL caches (106, 107, 205), preserves rank-5 <24h entry (105), newest 4/family, and Go/Playwright
: > "${work}/deletes-live.log"
out="$(PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-live.log" PRUNE_NOW="${now_epoch}" PRUNE_DRY_RUN=0 bash "${script}")"
actual="$(sort "${work}/deletes-live.log")"
expected="$(printf '%s\n' \
	'repos/eshu-hq/eshu/actions/caches/106' \
	'repos/eshu-hq/eshu/actions/caches/107' \
	'repos/eshu-hq/eshu/actions/caches/205')"
if [[ "${actual}" == "${expected}" ]] && [[ "${out}" == *"deleted 3 stale cache(s)"* ]]; then
	ok "live run deletes exact stale CodeQL IDs (106, 107, 205) and preserves rank-5 <24h entry + newest 4/family + Go/Playwright"
else
	bad "unexpected deletes: got=[${actual}] want=[${expected}] out=${out}"
fi

# 5. Fails closed when listing fails and executes zero DELETEs
: > "${work}/deletes-list-err.log"
set +e
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-list-err.log" FAKE_GH_FAIL_MODE=list PRUNE_NOW="${now_epoch}" bash "${script}" >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 1 ]] && [[ ! -s "${work}/deletes-list-err.log" ]]; then ok "fails closed when gh api list fails with zero DELETEs"; else bad "expected exit 1 and 0 deletes on list failure, got rc=${rc}"; fi

# 6. Fails closed when delete fails
set +e
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-err.log" FAKE_GH_FAIL_MODE=delete PRUNE_NOW="${now_epoch}" bash "${script}" >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 1 ]]; then ok "fails closed when gh api DELETE fails"; else bad "expected exit 1 on delete failure, got ${rc}"; fi

printf '\n%d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
