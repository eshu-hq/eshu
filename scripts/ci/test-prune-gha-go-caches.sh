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

# Two-page paginated fixture (10 eligible refs/heads/main Linux-X64 Go caches):
# - Linux-X64-gobuild (main): 101 (4h -> kept), 102 (10h <12h -> kept),
#   103 (28h -> deleted), 104 (5d -> deleted)
# - Linux-X64-gobuildrace (main): 201 (v2, 6h -> kept), 202 (v1, 4d -> deleted)
# - Linux-X64-golangcilint (main): 301 (20m <1h grace -> keeps 302 alive!)
# - Linux-X64-go (main): 401 (4h -> kept), 402 (10d -> deleted; exercises Linux-X64-go- arm)
# - Non-main ref (PR): 601 (2h old PR cache -> MUST NEVER supersede main or be deleted),
#   602 (missing ref -> MUST NEVER match)
# - Non-Go / macOS caches (501..505, 30d old -> MUST NEVER be deleted)
printf '%s\n' \
	'{"actions_caches":[' \
	'  {"id":101,"ref":"refs/heads/main","key":"Linux-X64-gobuild-f8f3-2026-11-28","size_in_bytes":1650000000,"created_at":"2026-11-28T01:17:00.000Z"},' \
	'  {"id":102,"ref":"refs/heads/main","key":"Linux-X64-gobuild-f8f3-2026-11-27b","size_in_bytes":1640000000,"created_at":"2026-11-27T19:17:00.000Z"},' \
	'  {"id":103,"ref":"refs/heads/main","key":"Linux-X64-gobuild-f8f3-2026-11-27","size_in_bytes":1639000000,"created_at":"2026-11-27T01:17:00.000Z"},' \
	'  {"id":104,"ref":"refs/heads/main","key":"Linux-X64-gobuild-08947048","size_in_bytes":1639000000,"created_at":"2026-11-23T23:40:51.000Z"},' \
	'  {"id":201,"ref":"refs/heads/main","key":"Linux-X64-gobuildrace-v2-f8f3","size_in_bytes":1041000000,"created_at":"2026-11-27T23:17:00.000Z"},' \
	'  {"id":202,"ref":"refs/heads/main","key":"Linux-X64-gobuildrace-08947048","size_in_bytes":533000000,"created_at":"2026-11-23T23:21:53.000Z"},' \
	'  {"id":601,"ref":"refs/pull/999/merge","key":"Linux-X64-golangcilint-v2.12.2-abc-2026-11-28","size_in_bytes":8500000,"created_at":"2026-11-28T03:00:00.000Z"},' \
	'  {"id":602,"key":"Linux-X64-gobuild-no-ref","size_in_bytes":1600000000,"created_at":"2026-11-20T00:00:00.000Z"},' \
	'  {"id":501,"ref":"refs/heads/main","key":"codeql-overlay-base-database-1-abc-javascript-a1","size_in_bytes":140000000,"created_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":502,"ref":"refs/heads/main","key":"playwright-chromium-Linux-deadbeef","size_in_bytes":270000000,"created_at":"2026-10-20T00:00:00.000Z"}' \
	']}' \
	'{"actions_caches":[' \
	'  {"id":301,"ref":"refs/heads/main","key":"Linux-X64-golangcilint-v2.12.2-abc-2026-11-28","size_in_bytes":8500000,"created_at":"2026-11-28T04:57:00.000Z"},' \
	'  {"id":302,"ref":"refs/heads/main","key":"Linux-X64-golangcilint-v2.12.2-abc-2026-11-27","size_in_bytes":8500000,"created_at":"2026-11-27T15:13:49.000Z"},' \
	'  {"id":401,"ref":"refs/heads/main","key":"Linux-X64-go-f8f3","size_in_bytes":491000000,"created_at":"2026-11-28T01:17:00.000Z"},' \
	'  {"id":402,"ref":"refs/heads/main","key":"Linux-X64-go-08947048","size_in_bytes":491000000,"created_at":"2026-11-18T00:00:00.000Z"},' \
	'  {"id":503,"ref":"refs/heads/main","key":"macOS-ARM64-gobuild-08947048","size_in_bytes":808000000,"created_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":504,"ref":"refs/heads/main","key":"cache-trivy-2026-10-20","size_in_bytes":103000000,"created_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":505,"ref":"refs/heads/main","key":"node-cache-Linux-x64-npm-deadbeef","size_in_bytes":96000000,"created_at":"2026-10-20T00:00:00.000Z"}' \
	']}' > "${work}/fixture.json"

# Solo-entry + PR-ref fixture: single 30d-old main entry + newer PR-ref entry -> 0 deleted
printf '%s\n' \
	'{"actions_caches":[' \
	'  {"id":701,"ref":"refs/heads/main","key":"Linux-X64-gobuild-solo","size_in_bytes":1600000000,"created_at":"2026-10-20T00:00:00.000Z"},' \
	'  {"id":702,"ref":"refs/pull/42/merge","key":"Linux-X64-gobuild-pr","size_in_bytes":1600000000,"created_at":"2026-11-28T01:00:00.000Z"}' \
	']}' > "${work}/solo-pr-fixture.json"

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

# 3. Dry-run lists exact 4 superseded Go caches and issues zero DELETEs
: > "${work}/deletes-dry.log"
out="$(PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-dry.log" PRUNE_NOW="${now_epoch}" PRUNE_DRY_RUN=1 bash "${script}")"
if [[ ! -s "${work}/deletes-dry.log" ]] && [[ "${out}" == *"(10 Linux-X64 Go); would delete 4 superseded cache(s)"* ]]; then
	ok "dry-run issues zero DELETEs and reports 10 eligible Go caches / 4 superseded"
else
	bad "dry-run failed: out=${out}"
fi

# 4. Live run deletes exact superseded main IDs (103, 104, 202, 402), protects <12h (102), <1h grace (302), PR/missing refs (601, 602), and non-Go/macOS (501..505)
: > "${work}/deletes-live.log"
out="$(PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-live.log" PRUNE_NOW="${now_epoch}" PRUNE_DRY_RUN=0 bash "${script}")"
actual="$(sort "${work}/deletes-live.log")"
expected="$(printf '%s\n' \
	'repos/eshu-hq/eshu/actions/caches/103' \
	'repos/eshu-hq/eshu/actions/caches/104' \
	'repos/eshu-hq/eshu/actions/caches/202' \
	'repos/eshu-hq/eshu/actions/caches/402')"
if [[ "${actual}" == "${expected}" ]] && [[ "${out}" == *"(10 Linux-X64 Go); deleted 4 superseded cache(s)"* ]]; then
	ok "live run deletes exact superseded main Go IDs (103, 104, 202, 402) and respects min_age, 1h grace, main-ref filter, and allowlist"
else
	bad "unexpected deletes: got=[${actual}] want=[${expected}] out=${out}"
fi

# 5. Newer PR-ref cache never supersedes a solo 30-day-old main entry
: > "${work}/deletes-solo.log"
out_solo="$(PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/solo-pr-fixture.json" FAKE_GH_DELETES="${work}/deletes-solo.log" PRUNE_NOW="${now_epoch}" PRUNE_DRY_RUN=0 bash "${script}")"
if [[ ! -s "${work}/deletes-solo.log" ]] && [[ "${out_solo}" == *"(1 Linux-X64 Go); deleted 0 superseded cache(s)"* ]]; then
	ok "newer PR-ref cache cannot supersede or delete solo refs/heads/main cache"
else
	bad "solo+PR test failed: deletes=[$(cat "${work}/deletes-solo.log")] out=${out_solo}"
fi

# 6. Once newest main golangcilint entry passes 1h grace (+3600s), superseded 302 IS pruned
: > "${work}/deletes-grace.log"
later_epoch=$((now_epoch + 3600))
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-grace.log" PRUNE_NOW="${later_epoch}" PRUNE_DRY_RUN=0 bash "${script}" >/dev/null
if awk '$0 == "repos/eshu-hq/eshu/actions/caches/302" { f=1 } END { exit !f }' "${work}/deletes-grace.log"; then
	ok "deletes superseded golangcilint entry (302) once newest main replacement passes 1h grace window"
else
	bad "expected 302 to be pruned after 1h grace: $(cat "${work}/deletes-grace.log")"
fi

# 7. Fails closed when listing fails (0 DELETEs)
: > "${work}/deletes-list-err.log"
set +e
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-list-err.log" FAKE_GH_FAIL_MODE=list PRUNE_NOW="${now_epoch}" bash "${script}" >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 1 ]] && [[ ! -s "${work}/deletes-list-err.log" ]]; then ok "fails closed when gh api list fails with zero DELETEs"; else bad "expected exit 1 and 0 deletes on list failure, got rc=${rc}"; fi

# 8. Fails closed when DELETE fails
set +e
PATH="${work}/bin:${PATH}" FAKE_GH_FIXTURE="${work}/fixture.json" FAKE_GH_DELETES="${work}/deletes-err.log" FAKE_GH_FAIL_MODE=delete PRUNE_NOW="${now_epoch}" bash "${script}" >/dev/null 2>&1
rc=$?
set -e
if [[ "${rc}" -eq 1 ]]; then ok "fails closed when gh api DELETE fails"; else bad "expected exit 1 on delete failure, got ${rc}"; fi

printf '\n%d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
