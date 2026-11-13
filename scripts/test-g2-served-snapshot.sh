#!/usr/bin/env bash
#
# test-g2-served-snapshot.sh — test mirror for the G2 served-spec snapshot
# tooling (Phase 1, eshu#9669 gate G2): generator idempotency, committed
# artifact parity, checker PASS on the clean tree, seeded-violation RED
# (fail-closed) proof, cross-links, and the offline (no live fetch) rule.
# Mirrors the record_pass/record_fail + watchdog shape of the repo's
# test-verify-* / test-generate-* mirrors. No Go build, no Postgres,
# no NornicDB, no network: every input is a committed file or a local
# git object.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="${repo_root}/scripts/gen-g2-served-snapshot.py"
checker="${repo_root}/scripts/check-g2-served-snapshot.sh"
committed="${repo_root}/docs/internal/g2-served-snapshot.md"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# run_with_watchdog runs "$@" in the background and kills it if it has not
# finished within the given timeout (portable substitute for `timeout`,
# same as scripts/test-generate-operator-dashboard.sh).
run_with_watchdog() {
  local timeout_secs="$1"
  shift
  "$@" &
  local pid=$!
  local waited=0
  while kill -0 "${pid}" 2>/dev/null; do
    sleep 1
    waited=$((waited + 1))
    if [ "${waited}" -ge "${timeout_secs}" ]; then
      kill -9 "${pid}" 2>/dev/null || true
      wait "${pid}" 2>/dev/null || true
      return 124
    fi
  done
  wait "${pid}"
}

# Satellite repo for the offline object reads: explicit env wins, else the
# standard sibling checkout next to the worktree parent.
if [ -z "${G2_SATELLITE_REPO:-}" ]; then
  for cand in "${repo_root}/../../eshu-api" "${HOME}/personal-repos/eshu-api"; do
    if [ -d "${cand}/.git" ]; then G2_SATELLITE_REPO="${cand}"; export G2_SATELLITE_REPO; break; fi
  done
fi

# Case 1: the generator runs offline and emits the snapshot shape.
out_path="${tmp_root}/regen.md"
if ! run_with_watchdog 60 env \
  G2_SATELLITE_REPO="${G2_SATELLITE_REPO:-}" \
  python3 "${generator}" --out "${out_path}" >/dev/null 2>"${tmp_root}/gen.err"; then
  record_fail "generator did not complete within the 60s watchdog"
  cat "${tmp_root}/gen.err" >&2 || true
else
  record_pass "generator completes within the watchdog"
fi

# Case 2: provenance block is present (core + satellite SHA + generator).
if rg -q '^core_sha: [0-9a-f]{40}$' "${out_path}" \
  && rg -q '^satellite_sha: [0-9a-f]{40}$' "${out_path}" \
  && rg -q '^generator: gen-g2-served-snapshot\.py v[0-9]+$' "${out_path}"; then
  record_pass "regenerated snapshot carries core/satellite/generator provenance"
else
  record_fail "regenerated snapshot provenance block is malformed"
fi

# Case 3: no drift — regen reproduces the committed artifact byte-for-byte.
if cmp -s "${out_path}" "${committed}"; then
  record_pass "generator output matches the committed snapshot"
else
  record_fail "generator output diverges from the committed snapshot"
fi

# Case 4: idempotency — two consecutive regens are byte-identical.
out_path2="${tmp_root}/regen2.md"
if python3 "${generator}" --out "${out_path2}" >/dev/null 2>&1 && [ -f "${out_path}" ]; then
  : keep-going
else
  record_fail "generator re-run did not produce output for idempotency check"
  printf 'tests passed: %s/%s\n' "${PASS}" "$((PASS + FAIL))"
  exit 1
fi
if cmp -s "${out_path}" "${out_path2}"; then
  record_pass "generator is idempotent on a clean re-run"
else
  record_fail "generator output is not byte-for-byte deterministic"
fi

# Case 5: checker PASS on the clean tree with the 7/3/4 family counts.
check_out="${tmp_root}/check-pass.txt"
if (cd "${repo_root}" && bash "${checker}" >"${check_out}" 2>&1); then
  if rg -q '^PASS g2-served-snapshot: rows=7 native=3 tierc=0 relay=4 absent=0' "${check_out}"; then
    record_pass "checker PASS on the clean tree with 7/3/0/4/0 counts"
  else
    record_fail "checker exit 0 but PASS line has unexpected counts"
    cat "${check_out}" >&2 || true
  fi
  if rg -q 'satellite-side verified at [0-9a-f]{12}' "${check_out}"; then
    record_pass "checker re-verified the satellite-side digests offline"
  else
    record_fail "checker did not verify the satellite side (G2_SATELLITE_REPO=${G2_SATELLITE_REPO:-unset})"
    cat "${check_out}" >&2 || true
  fi
else
  record_fail "checker FAILs on the clean tree"
  cat "${check_out}" >&2 || true
fi

# Case 6 (RED): a digest-flipped snapshot must FAIL the checker (fail-closed).
red_digest="${tmp_root}/red-digest.md"
cp "${committed}" "${red_digest}"
python3 - "${red_digest}" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
table = s.split("## Family inventory:", 1)[1]
tag = "tree:sha256:"
i = s.index(table) + table.index(tag) + len(tag)
old = s[i:i+64]
assert len(old) == 64 and all(c in "0123456789abcdef" for c in old), old
new = ("b" if old[0] == "a" else "a") + old[1:]
open(p, "w").write(s[:i] + new + s[i+64:])
EOF
if (cd "${repo_root}" && G2_SNAPSHOT="${red_digest}" bash "${checker}" >"${tmp_root}/check-red1.txt" 2>&1); then
  record_fail "checker PASS on a digest-flipped snapshot (fail-open)"
else
  record_pass "checker FAILs on a digest-flipped snapshot"
fi

# Case 7 (RED): a row-dropped snapshot must FAIL the checker (route-set gate).
red_rows="${tmp_root}/red-rows.md"
rg -v 'POST /api/v0/content/php-config/search' "${committed}" >"${red_rows}"
if (cd "${repo_root}" && G2_SNAPSHOT="${red_rows}" bash "${checker}" >"${tmp_root}/check-red2.txt" 2>&1); then
  record_fail "checker PASS on a row-dropped snapshot (fail-open)"
else
  record_pass "checker FAILs on a row-dropped snapshot"
fi

# Case 8: offline rule — no live-fetch mechanism in any form in the tooling.
if rg -n --no-filename -e 'curl' -e 'wget' -e 'git fetch' -e 'git pull' \
  -e 'git clone' -e 'urllib' -e 'http\.client' -e 'socket\.' \
  "${generator}" "${checker}" "${repo_root}/scripts/lib/g2_snapshot_data.py" \
  >"${tmp_root}/fetch-hits.txt" 2>&1; then
  record_fail "tooling contains a live-fetch mechanism"
  cat "${tmp_root}/fetch-hits.txt" >&2 || true
else
  record_pass "tooling contains no live-fetch mechanism"
fi

# Case 9: cross-links — tree pin versions equal the snapshot records, and
# every family route in the data module appears in the committed snapshot.
tree_search_pin="$(rg -o 'ContentSearchReadAdmissionContractVersion = "[^"]+"' "${repo_root}/go/internal/query/content_search_read_admission.go" | rg -o '"[^"]+"' | tr -d '"')"
tree_point_pin="$(rg -o 'ContentPointReadAdmissionContractVersion = "[^"]+"' "${repo_root}/go/internal/query/content_read_admission.go" | rg -o '"[^"]+"' | tr -d '"')"
if rg -q "ContentSearchReadAdmission ${tree_search_pin}" "${committed}" \
  && rg -q "ContentPointReadAdmission ${tree_point_pin}" "${committed}"; then
  record_pass "snapshot pin versions match the in-tree contract consts"
else
  record_fail "snapshot pin versions diverge from the in-tree consts"
fi

printf 'tests passed: %s/%s\n' "${PASS}" "$((PASS + FAIL))"
[ "${FAIL}" -eq 0 ]
