#!/usr/bin/env bash
#
# test-g2-served-snapshot.sh — test mirror for the G2 served-spec snapshot
# tooling (v2 full inventory, eshu#9669 gate G2): generator idempotency,
# committed artifact parity, checker behavior on the frozen tree (GREEN with
# 0 non-survivors), seeded-violation RED (fail-closed)
# proof, cross-links, and the offline (no live fetch) rule. No Go build,
# no Postgres, no NornicDB, no network: every input is a committed file
# or a local git object.
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
if ! run_with_watchdog 120 env \
  G2_SATELLITE_REPO="${G2_SATELLITE_REPO:-}" \
  python3 "${generator}" --out "${out_path}" >/dev/null 2>"${tmp_root}/gen.err"; then
  record_fail "generator did not complete within the 120s watchdog"
  cat "${tmp_root}/gen.err" >&2 || true
else
  record_pass "generator completes within the watchdog"
fi

# Case 2: provenance block is present (core + satellite SHA + generator v2).
if rg -q '^core_sha: [0-9a-f]{40}$' "${out_path}" \
  && rg -q '^satellite_sha: [0-9a-f]{40}$' "${out_path}" \
  && rg -q '^generator: gen-g2-served-snapshot\.py v2$' "${out_path}"; then
  record_pass "regenerated snapshot carries core/satellite/generator-v2 provenance"
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

# Case 5: full-inventory PASS line with v2 counts is recorded.
if rg -q '^PASS g2-served-snapshot: rows=282 native=236 tierc=27 relay=19 absent=0 nonsurvivor=0$' "${committed}"; then
  record_pass "snapshot records v2 full-inventory counts (282/236/27/19/0, nonsurvivor=0)"
else
  record_fail "snapshot PASS line lacks v2 reconciled counts"
fi

# Case 6: checker exits 0 on the frozen tree with zero non-survivor
# relay rows and zero absent rows (the G2 pass bar).
check_out="${tmp_root}/check-pass.txt"
if (cd "${repo_root}" && bash "${checker}" >"${check_out}" 2>&1); then
  if rg -q 'nonsurvivor=0' "${check_out}"; then
    record_pass "checker exits 0 with nonsurvivor=0 (G2 passes)"
  else
    record_fail "checker exit 0 without the nonsurvivor=0 PASS line"
    cat "${check_out}" >&2 || true
  fi
else
  record_fail "checker FAILs on the passing tree"
  cat "${check_out}" >&2 || true
fi

# Case 7: checker re-verified the satellite-side digests offline.
if (cd "${repo_root}" && G2_SATELLITE_REPO="${G2_SATELLITE_REPO:-}" bash "${checker}" >"${tmp_root}/check-sat.txt" 2>&1) \
  || rg -q 'satellite-side verified at [0-9a-f]{12}' "${tmp_root}/check-sat.txt"; then
  if rg -q 'satellite-side verified at [0-9a-f]{12}' "${tmp_root}/check-sat.txt"; then
    record_pass "checker re-verified the satellite-side digests offline"
  else
    record_fail "checker did not verify the satellite side (G2_SATELLITE_REPO=${G2_SATELLITE_REPO:-unset})"
    cat "${tmp_root}/check-sat.txt" >&2 || true
  fi
else
  record_fail "checker errored before the satellite-side note"
  cat "${tmp_root}/check-sat.txt" >&2 || true
fi

# Case 8 (RED): a digest-flipped snapshot must FAIL the checker (fail-closed).
red_digest="${tmp_root}/red-digest.md"
cp "${committed}" "${red_digest}"
python3 - "${red_digest}" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
table = s.split("## Full inventory", 1)[1]
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

# Case 9 (RED): a row-dropped snapshot must FAIL the checker (route-set gate).
red_rows="${tmp_root}/red-rows.md"
rg -v 'POST /api/v0/content/php-config/search' "${committed}" >"${red_rows}"
if (cd "${repo_root}" && G2_SNAPSHOT="${red_rows}" bash "${checker}" >"${tmp_root}/check-red2.txt" 2>&1); then
  record_fail "checker PASS on a row-dropped snapshot (fail-open)"
else
  record_pass "checker FAILs on a row-dropped snapshot"
fi

# Case 10 (RED): a survivor flipped to native must FAIL (exemption integrity).
red_surv="${tmp_root}/red-surv.md"
cp "${committed}" "${red_surv}"
python3 - "${red_surv}" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
old = "| `PATCH /api/v0/auth/admin/sign-in-policy` | relay-disclosed |"
assert old in s, "survivor row shape changed"
s = s.replace(old, "| `PATCH /api/v0/auth/admin/sign-in-policy` | native-served |", 1)
open(p, "w").write(s)
EOF
if (cd "${repo_root}" && G2_SNAPSHOT="${red_surv}" bash "${checker}" >"${tmp_root}/check-red3.txt" 2>&1); then
  record_fail "checker PASS on a survivor-flipped snapshot (fail-open)"
else
  record_pass "checker FAILs on a survivor-flipped snapshot"
fi

# Case 11: offline rule — no live-fetch mechanism in any form in the tooling.
if rg -n --no-filename -e 'curl' -e 'wget' -e 'git fetch' -e 'git pull' \
  -e 'git clone' -e 'urllib' -e 'http\.client' -e 'socket\.' \
  "${generator}" "${checker}" "${repo_root}/scripts/lib/g2_snapshot_data.py" \
  "${repo_root}/scripts/lib/g2_snapshot_derive.py" \
  "${repo_root}/scripts/lib/g2_snapshot_gate.py" \
  >"${tmp_root}/fetch-hits.txt" 2>&1; then
  record_fail "tooling contains a live-fetch mechanism"
  cat "${tmp_root}/fetch-hits.txt" >&2 || true
else
  record_pass "tooling contains no live-fetch mechanism"
fi

# Case 12: cross-links — every cited pin const exists at v0 in-tree.
bad_pin=0
while IFS= read -r pin; do
  pin=$(printf '%s' "${pin}" | sed -e 's/^ *//' -e 's/ *$//')
  [ "${pin}" = "none" ] && continue
  name=${pin% *}; ver=${pin#* }
  case "${name}" in
    *Version) const="${name}" ;;
    *) const="${name}ContractVersion" ;;
  esac
  if ! rg -q -F "${const} = \"${ver}\"" "${repo_root}/go" 2>/dev/null; then
    printf 'missing pin const: %s\n' "${pin}" >&2
    bad_pin=1
  fi
done < <(awk -F'|' '/^\| `(GET|POST|PATCH|PUT|DELETE) / {print $6}' "${committed}")
if [ "${bad_pin}" -eq 0 ]; then
  record_pass "every cited pin const exists at the cited version in-tree"
else
  record_fail "snapshot cites a pin const missing in-tree"
fi

printf 'tests passed: %s/%s\n' "${PASS}" "$((PASS + FAIL))"
[ "${FAIL}" -eq 0 ]
