#!/usr/bin/env bash
# Hermetic self-test for scripts/ci/prune-gha-codeql-caches.sh (zero network,
# mocked gh binary, zero rg dependency).
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
script="${repo_root}/scripts/ci/prune-gha-codeql-caches.sh"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

pass=0
fail=0
ok() { pass=$((pass + 1)); printf 'ok - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s\n' "$1" >&2; }

# Mock gh CLI controlled by FAKE_GH_FIXTURE and FAKE_GH_FAIL_MODE.
mkdir -p "${work}/bin"
cat > "${work}/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" != "api" ]]; then
	echo "unexpected gh subcommand: $*" >&2
	exit 99
fi
shift
if [[ "${1:-}" == "--paginate" ]]; then
	if [[ "${FAKE_GH_FAIL_MODE:-}" == "list" ]]; then
		echo "HTTP 403 Forbidden" >&2
		exit 1
	fi
	cat "${FAKE_GH_FIXTURE}"
	exit 0
fi
if [[ "${1:-}" == "--method" && "${2:-}" == "DELETE" ]]; then
	target="${3:-}"
	if [[ "${FAKE_GH_FAIL_MODE:-}" == "delete" ]]; then
		echo "HTTP 500 Internal Server Error" >&2
		exit 1
	fi
	printf '%s\n' "${target}" >> "${FAKE_GH_DELETES}"
	exit 0
fi
echo "unexpected gh api args: $*" >&2
exit 99
EOF
chmod +x "${work}/bin/gh"

# Fixed "now": 2026-11-27T00:00:00Z = 1795737600
now_epoch=1795737600

# Two-page paginated fixture:
# - 6 javascript overlay caches (4 newer + 2 stale >24h old -> expect 2 deleted: ids 105, 106)
# - 5 trap caches (all >24h old -> newest 4 kept, oldest 1 deleted: id 205)
# - 3 python overlay caches (all >24h old -> <=4 so 0 deleted)
# - 3 non-CodeQL caches (30 days old -> MUST NEVER be deleted: ids 301, 302, 303)
cat > "${work}/fixture.json" <<'EOF'
{"actions_caches":[
  {"id":101,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a1","size_in_bytes":140000000,"last_accessed_at":"2026-11-26T23:00:00.000Z"},
  {"id":102,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a2","size_in_bytes":140000000,"last_accessed_at":"2026-11-26T20:00:00.000Z"},
  {"id":103,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a3","size_in_bytes":140000000,"last_accessed_at":"2026-11-26T18:00:00.000Z"},
  {"id":104,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.6-a4","size_in_bytes":140000000,"last_accessed_at":"2026-11-25T10:00:00.000Z"},
  {"id":105,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.5-a5","size_in_bytes":140000000,"last_accessed_at":"2026-11-25T09:00:00.000Z"},
  {"id":106,"key":"codeql-overlay-base-database-1-abc123-javascript-2.27.5-a6","size_in_bytes":140000000,"last_accessed_at":"2026-11-25T08:00:00.000Z"},
  {"id":301,"key":"Linux-X64-gobuild-deadbeef","size_in_bytes":1600000000,"last_accessed_at":"2026-10-20T00:00:00.000Z"},
  {"id":302,"key":"Linux-X64-gobuildrace-deadbeef","size_in_bytes":530000000,"last_accessed_at":"2026-10-20T00:00:00.000Z"},
  {"id":303,"key":"playwright-chromium-Linux-deadbeef","size_in_bytes":270000000,"last_accessed_at":"2026-10-20T00:00:00.000Z"}
]}
{"actions_caches":[
  {"id":201,"key":"codeql-trap-1-a","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T12:00:00.000Z"},
  {"id":202,"key":"codeql-trap-1-b","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T11:00:00.000Z"},
  {"id":203,"key":"codeql-trap-1-c","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T10:00:00.000Z"},
  {"id":204,"key":"codeql-trap-1-d","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T09:00:00.000Z"},
  {"id":205,"key":"codeql-trap-1-e","size_in_bytes":24000000,"last_accessed_at":"2026-11-25T08:00:00.000Z"},
  {"id":251,"key":"codeql-overlay-base-database-1-abc123-python-2.27.5-p1","size_in_bytes":27000000,"last_accessed_at":"2026-11-24T01:00:00.000Z"},
  {"id":252,"key":"codeql-overlay-base-database-1-abc123-python-2.27.5-p2","size_in_bytes":27000000,"last_accessed_at":"2026-11-24T02:00:00.000Z"},
  {"id":253,"key":"codeql-overlay-base-database-1-abc123-python-2.27.5-p3","size_in_bytes":27000000,"last_accessed_at":"2026-11-24T03:00:00.000Z"}
]}
EOF

# 1. Refuses positional arguments
set +e
PATH="${work}/bin:${PATH}" bash "${script}" extra-arg >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 2 ]]; then ok "rejects positional arguments with exit 2"; else bad "expected exit 2 on arg, got ${rc}"; fi

# 2. Rejects invalid PRUNE_MIN_AGE_HOURS
for bad_h in 0 -5 abc; do
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

# 4. Live run deletes exact 3 stale CodeQL caches (105, 106, 205) and never touches Go/Playwright/newest-4
: > "${work}/deletes-live.log"
out="$(PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-live.log" PRUNE_NOW="${now_epoch}" PRUNE_DRY_RUN=0 bash "${script}")"
actual="$(sort "${work}/deletes-live.log")"
expected="$(printf '%s\n' \
	'repos/eshu-hq/eshu/actions/caches/105' \
	'repos/eshu-hq/eshu/actions/caches/106' \
	'repos/eshu-hq/eshu/actions/caches/205')"
if [[ "${actual}" == "${expected}" ]] && [[ "${out}" == *"deleted 3 stale cache(s)"* ]]; then
	ok "live run deletes exact stale CodeQL IDs (105, 106, 205) and preserves newest 4/family + Go/Playwright"
else
	bad "unexpected deletes: got=[${actual}] want=[${expected}] out=${out}"
fi

# 5. Fails closed when listing fails
set +e
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-err.log" FAKE_GH_FAIL_MODE=list PRUNE_NOW="${now_epoch}" bash "${script}" >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 1 ]]; then ok "fails closed when gh api list fails"; else bad "expected exit 1 on list failure, got ${rc}"; fi

# 6. Fails closed when delete fails
set +e
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-err.log" FAKE_GH_FAIL_MODE=delete PRUNE_NOW="${now_epoch}" bash "${script}" >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 1 ]]; then ok "fails closed when gh api DELETE fails"; else bad "expected exit 1 on delete failure, got ${rc}"; fi

printf '\n%d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
