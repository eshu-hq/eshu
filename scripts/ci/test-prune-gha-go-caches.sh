#!/usr/bin/env bash
# Hermetic self-test for scripts/ci/prune-gha-go-caches.sh (zero network,
# mocked gh binary, zero rg dependency, zero heredocs over 512 bytes).
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
script="${repo_root}/scripts/ci/prune-gha-go-caches.sh"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

pass=0
fail=0
ok() { pass=$((pass + 1)); printf 'ok - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s\n' "$1" >&2; }

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

# Fixed "now": 2026-11-28T05:17:00Z = 1795843020
now_epoch=1795843020

# Two-page paginated fixture:
# - Linux-X64-gobuild: 101 (4h old -> kept), 102 (10h old -> <12h min_age, kept),
#   103 (28h old -> deleted), 104 (5d old legacy -> deleted)
# - Linux-X64-gobuildrace: 201 (v2, 6h old -> kept), 202 (v1, 4d old -> deleted)
# - Linux-X64-golangcilint: 301 (20m old -> <1h grace, so 302 MUST survive!)
# - Linux-X64-go: 401 (only 1 entry, 10d old -> MUST NEVER be deleted)
# - Non-Go caches (501..505, 30d old -> MUST NEVER be deleted)
printf '%s\n' \
	'{"actions_caches":[' \
	'  {"id":101,"key":"Linux-X64-gobuild-f8f3-2026-11-28","size_in_bytes":1650000000,"created_at":"2026-11-28T01:17:00.000Z"},' \
	'  {"id":102,"key":"Linux-X64-gobuild-f8f3-2026-11-27b","size_in_bytes":1640000000,"created_at":"2026-11-27T19:17:00.000Z"},' \
	'  {"id":103,"key":"Linux-X64-gobuild-f8f3-2026-11-27","size_in_bytes":1639000000,"created_at":"2026-11-27T01:17:00.000Z"},' \
	'  {"id":104,"key":"Linux-X64-gobuild-08947048","size_in_bytes":1639000000,"created_at":"2026-11-23T23:40:51.000Z"},' \
	'  {"id":201,"key":"Linux-X64-gobuildrace-v2-f8f3","size_in_bytes":1041000000,"created_at":"2026-11-27T23:17:00.000Z"},' \
	'  {"id":202,"key":"Linux-X64-gobuildrace-08947048","size_in_bytes":533000000,"created_at":"2026-11-23T23:21:53.000Z"},' \
	'  {"id":501,"key":"codeql-overlay-base-database-1-abc-javascript-a1","size_in_bytes":140000000,"created_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":502,"key":"playwright-chromium-Linux-deadbeef","size_in_bytes":270000000,"created_at":"2026-10-20T00:00:00.000Z"}' \
	']}' \
	'{"actions_caches":[' \
	'  {"id":301,"key":"Linux-X64-golangcilint-v2.12.2-abc-2026-11-28","size_in_bytes":8500000,"created_at":"2026-11-28T04:57:00.000Z"},' \
	'  {"id":302,"key":"Linux-X64-golangcilint-v2.12.2-abc-2026-11-27","size_in_bytes":8500000,"created_at":"2026-11-27T15:13:49.000Z"},' \
	'  {"id":401,"key":"Linux-X64-go-08947048","size_in_bytes":491000000,"created_at":"2026-11-18T00:00:00.000Z"},' \
	'  {"id":503,"key":"macOS-ARM64-gobuild-08947048","size_in_bytes":808000000,"created_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":504,"key":"cache-trivy-2026-10-20","size_in_bytes":103000000,"created_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":505,"key":"node-cache-Linux-x64-npm-deadbeef","size_in_bytes":96000000,"created_at":"2026-10-20T00:00:00.000Z"}' \
	']}' > "${work}/fixture.json"

# 1. Refuses positional arguments
set +e
PATH="${work}/bin:${PATH}" bash "${script}" extra-arg >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 2 ]]; then ok "rejects positional arguments with exit 2"; else bad "expected exit 2 on arg, got ${rc}"; fi

# 2. Rejects invalid PRUNE_GO_MIN_AGE_HOURS
for bad_h in 0 -5 abc 08 010; do
	set +e
	PATH="${work}/bin:${PATH}" PRUNE_GO_MIN_AGE_HOURS="${bad_h}" bash "${script}" >/dev/null 2>&1
	rc=$?
	set -e
	if [[ "${rc}" -eq 2 ]]; then ok "rejects PRUNE_GO_MIN_AGE_HOURS=${bad_h}"; else bad "expected exit 2 for hours=${bad_h}, got ${rc}"; fi
done

# 3. Dry-run lists exact 3 superseded Go caches and issues zero DELETEs
: > "${work}/deletes-dry.log"
out="$(PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-dry.log" PRUNE_NOW="${now_epoch}" PRUNE_DRY_RUN=1 bash "${script}")"
if [[ ! -s "${work}/deletes-dry.log" ]] && [[ "${out}" == *"would delete 3 superseded cache(s)"* ]]; then
	ok "dry-run issues zero DELETEs and reports 3 superseded Go caches"
else
	bad "dry-run failed: out=${out}"
fi

# 4. Live run deletes exact superseded IDs (103, 104, 202), protects <12h (102), <1h grace (302), solo family (401), and non-Go/macOS (501..505)
: > "${work}/deletes-live.log"
out="$(PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-live.log" PRUNE_NOW="${now_epoch}" PRUNE_DRY_RUN=0 bash "${script}")"
actual="$(sort "${work}/deletes-live.log")"
expected="$(printf '%s\n' \
	'repos/eshu-hq/eshu/actions/caches/103' \
	'repos/eshu-hq/eshu/actions/caches/104' \
	'repos/eshu-hq/eshu/actions/caches/202')"
if [[ "${actual}" == "${expected}" ]] && [[ "${out}" == *"deleted 3 superseded cache(s)"* ]]; then
	ok "live run deletes exact superseded Go IDs (103, 104, 202) and respects min_age, 1h grace, solo-entry, and allowlist"
else
	bad "unexpected deletes: got=[${actual}] want=[${expected}] out=${out}"
fi

# 5. Once newest golangcilint entry passes 1h grace (+3600s), superseded 302 IS pruned
: > "${work}/deletes-grace.log"
later_epoch=$((now_epoch + 3600))
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-grace.log" PRUNE_NOW="${later_epoch}" PRUNE_DRY_RUN=0 bash "${script}" >/dev/null
if awk '$0 == "repos/eshu-hq/eshu/actions/caches/302" { f=1 } END { exit !f }' "${work}/deletes-grace.log"; then
	ok "deletes superseded golangcilint entry (302) once newest replacement passes 1h grace window"
else
	bad "expected 302 to be pruned after 1h grace: $(cat "${work}/deletes-grace.log")"
fi

# 6. Fails closed when listing fails (0 DELETEs)
: > "${work}/deletes-list-err.log"
set +e
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-list-err.log" FAKE_GH_FAIL_MODE=list PRUNE_NOW="${now_epoch}" bash "${script}" >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 1 ]] && [[ ! -s "${work}/deletes-list-err.log" ]]; then ok "fails closed when gh api list fails with zero DELETEs"; else bad "expected exit 1 and 0 deletes on list failure, got rc=${rc}"; fi

# 7. Fails closed when DELETE fails
set +e
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-err.log" FAKE_GH_FAIL_MODE=delete PRUNE_NOW="${now_epoch}" bash "${script}" >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 1 ]]; then ok "fails closed when gh api DELETE fails"; else bad "expected exit 1 on delete failure, got ${rc}"; fi

printf '\n%d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
