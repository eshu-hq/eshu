#!/usr/bin/env bash
# test-verify-npm-audit.sh - hermetic self-test for scripts/verify-npm-audit.sh
# (the dated single-advisory allowlist wrapper around `npm audit`, #10854).
#
# Run with:
#   bash scripts/test-verify-npm-audit.sh
#
# Offline: every case feeds recorded `npm audit --json` reports (the real
# report for this lockfile lives in tests/fixtures/npm-audit/) through the
# wrapper's fixture mode, or runs live mode against a stub `npm` on PATH.
# Two mutants (expiry check removed, production-clean check removed) must
# turn at least one case red, which proves the cases bite.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fix="${repo_root}/tests/fixtures/npm-audit"
spec="${repo_root}/specs/npm-audit-allowlist.v1.json"
workflow="${repo_root}/.github/workflows/frontend.yml"
real_all="${fix}/real-all.json"
real_prod="${fix}/real-prod.json"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

for f in "${real_all}" "${real_prod}" "${spec}"; do
  [[ -f "${f}" ]] || { echo "test-verify-npm-audit: missing ${f}" >&2; exit 1; }
done

# mutate IN OUT JS - load IN as r, run JS, write r to OUT. The JS strings are
# literals in this test file (never external input).
mutate() {
  node -e 'const fs=require("fs");const r=JSON.parse(fs.readFileSync(process.argv[1]));new Function("r",process.argv[3])(r);fs.writeFileSync(process.argv[2],JSON.stringify(r))' "$1" "$2" "$3"
}

# seed IN OUT PKG SEVERITY GHSA - add a direct advisory entry to a report.
seed() {
  mutate "$1" "$2" "const p='$3',s='$4',g='$5';r.vulnerabilities[p]={name:p,severity:s,isDirect:true,via:[{source:1,name:p,dependency:p,title:'seeded',url:'https://github.com/advisories/'+g,severity:s,cwe:[],cvss:{score:0,vectorString:null},range:'*'}],effects:[],range:'*',nodes:['node_modules/'+p],fixAvailable:false}"
}

# --- fixtures -------------------------------------------------------------
seed "${real_all}" "${tmp}/dev-high.json" lodash high GHSA-aaaa-bbbb-cccc
seed "${real_all}" "${tmp}/prod-high-all.json" leftpad high GHSA-dddd-eeee-ffff
seed "${real_prod}" "${tmp}/prod-high.json" leftpad high GHSA-dddd-eeee-ffff
seed "${real_all}" "${tmp}/critical.json" critpkg critical GHSA-gggg-hhhh-iiii
seed "${real_prod}" "${tmp}/prod-listed.json" picomatch high GHSA-fc5q-39xc-vjpf
mutate "${real_all}" "${tmp}/moderate-only.json" "r.vulnerabilities={minimatch:r.vulnerabilities.minimatch}"
mutate "${real_all}" "${tmp}/sev-raised.json" "const v=r.vulnerabilities.picomatch;v.severity='critical';v.via[0].severity='critical'"
printf '{"version":1,"entries":[]}\n' >"${tmp}/allow-empty.json"
printf '{"error":{"code":"ENOAUDIT","summary":"audit endpoint returned an error","detail":"x"}}\n' >"${tmp}/npm-error.json"
printf '{"auditReportVersion":2,"vulnerabilities":' >"${tmp}/broken.json"
: >"${tmp}/empty.json"
mutate "${spec}" "${tmp}/allow-long.json" "r.entries[0].expires='2026-12-19'"
mutate "${spec}" "${tmp}/allow-no-expiry.json" "delete r.entries[0].expires"
mutate "${spec}" "${tmp}/allow-extra-key.json" "r.entries[0].note='x'"
mutate "${spec}" "${tmp}/allow-wrong-pkg.json" "r.entries[0].package='other'"
mutate "${spec}" "${tmp}/allow-bad-date.json" "r.entries[0].added='2026-02-30'"
mutate "${spec}" "${tmp}/allow-dup.json" "r.entries.push(r.entries[0])"

# --- case runner ----------------------------------------------------------
fails=0
verbose=1
last_out=""

# case NAME EXPECT(pass|fail) SCRIPT ALL PROD ALLOW DATE
run_case() {
  local name="$1" expect="$2" script="$3" all="$4" prod="$5" allow="$6" date="$7"
  local rc=0
  last_out="$(bash "${script}" --fixture-all "${all}" --fixture-prod "${prod}" \
    --allowlist "${allow}" --test-date "${date}" 2>&1)" || rc=$?
  local got=fail
  [[ "${rc}" -eq 0 ]] && got=pass
  if [[ "${got}" == "${expect}" ]]; then
    [[ "${verbose}" -eq 1 ]] && echo "ok   ${name} (${expect}, rc=${rc})"
  else
    fails=$((fails + 1))
    [[ "${verbose}" -eq 1 ]] && { echo "FAIL ${name}: wanted ${expect}, got ${got} (rc=${rc})"; printf '%s\n' "${last_out}" | sed 's/^/     | /'; }
  fi
  return 0
}

suite() {
  local s="$1"
  # The ten ruling fixtures.
  run_case "real report passes today" pass "${s}" "${real_all}" "${real_prod}" "${spec}" 2026-11-17
  run_case "seeded high advisory in a production dependency fails" fail "${s}" "${tmp}/prod-high-all.json" "${tmp}/prod-high.json" "${spec}" 2026-11-17
  run_case "second unlisted dev-only high advisory fails" fail "${s}" "${tmp}/dev-high.json" "${real_prod}" "${spec}" 2026-11-17
  run_case "allowlisted advisory in the production report fails" fail "${s}" "${real_all}" "${tmp}/prod-listed.json" "${spec}" 2026-11-17
  run_case "date after expiry fails" fail "${s}" "${real_all}" "${real_prod}" "${spec}" 2026-12-18
  run_case "window over 31 days fails" fail "${s}" "${real_all}" "${real_prod}" "${tmp}/allow-long.json" 2026-11-17
  run_case "stale entry fails" fail "${s}" "${tmp}/moderate-only.json" "${real_prod}" "${spec}" 2026-11-17
  run_case "broken JSON fails" fail "${s}" "${tmp}/broken.json" "${real_prod}" "${spec}" 2026-11-17
  run_case "moderate-only passes" pass "${s}" "${tmp}/moderate-only.json" "${real_prod}" "${tmp}/allow-empty.json" 2026-11-17
  run_case "unlisted critical fails" fail "${s}" "${tmp}/critical.json" "${real_prod}" "${spec}" 2026-11-17
}

extra_suite() {
  local s="$1"
  run_case "expiry day itself fails" fail "${s}" "${real_all}" "${real_prod}" "${spec}" 2026-12-17
  run_case "last day before expiry passes" pass "${s}" "${real_all}" "${real_prod}" "${spec}" 2026-12-16
  run_case "empty audit output fails" fail "${s}" "${tmp}/empty.json" "${real_prod}" "${spec}" 2026-11-17
  run_case "npm error object fails" fail "${s}" "${tmp}/npm-error.json" "${real_prod}" "${spec}" 2026-11-17
  run_case "broken production report fails" fail "${s}" "${real_all}" "${tmp}/broken.json" "${spec}" 2026-11-17
  run_case "missing report file fails" fail "${s}" "${tmp}/nope.json" "${real_prod}" "${spec}" 2026-11-17
  run_case "severity above the recorded one fails" fail "${s}" "${tmp}/sev-raised.json" "${real_prod}" "${spec}" 2026-11-17
  run_case "high advisory with empty allowlist fails" fail "${s}" "${real_all}" "${real_prod}" "${tmp}/allow-empty.json" 2026-11-17
  run_case "id match without package match fails" fail "${s}" "${real_all}" "${real_prod}" "${tmp}/allow-wrong-pkg.json" 2026-11-17
  run_case "entry without expiry fails" fail "${s}" "${real_all}" "${real_prod}" "${tmp}/allow-no-expiry.json" 2026-11-17
  run_case "entry with an unknown key fails" fail "${s}" "${real_all}" "${real_prod}" "${tmp}/allow-extra-key.json" 2026-11-17
  run_case "entry with an impossible date fails" fail "${s}" "${real_all}" "${real_prod}" "${tmp}/allow-bad-date.json" 2026-11-17
  run_case "duplicate entries fail" fail "${s}" "${real_all}" "${real_prod}" "${tmp}/allow-dup.json" 2026-11-17
  run_case "unparsable allowlist fails" fail "${s}" "${real_all}" "${real_prod}" "${tmp}/broken.json" 2026-11-17
  run_case "high advisory only in production fails" fail "${s}" "${real_all}" "${tmp}/prod-high.json" "${spec}" 2026-11-17
}

wrapper="${repo_root}/scripts/verify-npm-audit.sh"
if [[ ! -f "${wrapper}" ]]; then
  echo "test-verify-npm-audit: missing ${wrapper}" >&2
  exit 1
fi

echo "== ruling fixtures =="
suite "${wrapper}"
echo "== extra cases =="
extra_suite "${wrapper}"

# --- 7-day warning ----------------------------------------------------------
run_case "inside the 7-day window passes" pass "${wrapper}" "${real_all}" "${real_prod}" "${spec}" 2026-12-10
if ! printf '%s\n' "${last_out}" | rg -q '^::warning::.*2026-12-17'; then
  fails=$((fails + 1)); echo "FAIL expected ::warning:: naming 2026-12-17 at 2026-12-10"
fi
run_case "outside the 7-day window passes" pass "${wrapper}" "${real_all}" "${real_prod}" "${spec}" 2026-12-09
if printf '%s\n' "${last_out}" | rg -q '::warning::'; then
  fails=$((fails + 1)); echo "FAIL unexpected ::warning:: at 2026-12-09"
fi

# --- clock override only with fixtures ----------------------------------------
refuse() {
  local name="$1"
  shift
  local rc=0
  last_out="$(bash "${wrapper}" "$@" 2>&1)" || rc=$?
  if [[ "${rc}" -eq 2 ]]; then echo "ok   ${name} (rc=2)"; else fails=$((fails + 1)); echo "FAIL ${name}: rc=${rc}"; fi
}
refuse "test date without fixtures is refused" --test-date 2026-11-17
refuse "allowlist override without fixtures is refused" --allowlist "${spec}"
refuse "one fixture without the other is refused" --fixture-all "${real_all}" --test-date 2026-11-17
refuse "fixtures without a test date are refused" --fixture-all "${real_all}" --fixture-prod "${real_prod}"
refuse "unknown flag is refused" --nope

# --- live mode with a stub npm ----------------------------------------------
stub="${tmp}/stub"
mkdir -p "${stub}"
cat >"${stub}/npm" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"${STUB_LOG}"
case "$*" in
  *--omit=dev*) cat "${STUB_PROD}" ;;
  *) cat "${STUB_ALL}" ;;
esac
exit 1
STUB
chmod +x "${stub}/npm"

live() { # live ALL PROD [ENV=VALUE...] - echoes "rc|output"
  local all="$1" prod="$2" rc=0 out
  shift 2
  : >"${tmp}/npm.log"
  out="$(env "$@" PATH="${stub}:${PATH}" STUB_LOG="${tmp}/npm.log" STUB_ALL="${all}" STUB_PROD="${prod}" \
    bash "${wrapper}" 2>&1)" || rc=$?
  printf '%s|%s' "${rc}" "${out}"
}

# The workflow job runs live mode, so a clock env var must change nothing.
base="$(live "${real_all}" "${real_prod}" X=1)"
for v in NPM_AUDIT_TEST_DATE=2099-01-01 NPM_AUDIT_NOW=2000-01-01 FAKETIME=2099-01-01; do
  moved="$(live "${real_all}" "${real_prod}" "${v}")"
  if [[ "${base}" == "${moved}" ]]; then echo "ok   live mode ignores ${v%%=*}"; else
    fails=$((fails + 1)); echo "FAIL live mode changed with ${v}"; fi
done
live "${real_all}" "${real_prod}" X=1 >/dev/null
if rg -qx 'audit --json' "${tmp}/npm.log" && rg -qx 'audit --omit=dev --json' "${tmp}/npm.log"; then
  echo "ok   live mode runs npm audit --json and npm audit --omit=dev --json"
else
  fails=$((fails + 1)); echo "FAIL live mode npm invocations: $(tr '\n' ';' <"${tmp}/npm.log")"
fi
res="$(live "${tmp}/broken.json" "${real_prod}" X=1)"
if [[ "${res%%|*}" -ne 0 ]]; then echo "ok   live mode fails on unparsable npm output"; else
  fails=$((fails + 1)); echo "FAIL live mode passed on unparsable npm output"; fi
res="$(live "${tmp}/critical.json" "${real_prod}" X=1)"
if [[ "${res%%|*}" -ne 0 ]]; then echo "ok   live mode fails on an unlisted critical"; else
  fails=$((fails + 1)); echo "FAIL live mode passed on an unlisted critical"; fi

# --- workflow wiring -----------------------------------------------------------
if rg -q -- '--test-date|--fixture-all|--fixture-prod' "${repo_root}/.github/workflows"; then
  fails=$((fails + 1)); echo "FAIL a workflow passes a test-date or fixture flag"
else
  echo "ok   no workflow passes a test-date or fixture flag"
fi
if rg -q '^\s+run: bash scripts/verify-npm-audit\.sh$' "${workflow}"; then
  echo "ok   frontend.yml blocking step is the bare wrapper call"
else
  fails=$((fails + 1)); echo "FAIL frontend.yml blocking step is not 'run: bash scripts/verify-npm-audit.sh'"
fi
if rg -q 'verify-npm-audit\.sh.*\|\|' "${workflow}" || rg -q 'npm audit --audit-level=high' "${workflow}"; then
  fails=$((fails + 1)); echo "FAIL frontend.yml still has a bypassed or direct blocking audit"
else
  echo "ok   frontend.yml has no '|| true' on the wrapper and no direct --audit-level=high"
fi
if rg -q 'run: npm audit \|\| true' "${workflow}"; then
  echo "ok   non-blocking full report step is kept"
else
  fails=$((fails + 1)); echo "FAIL non-blocking full report step is missing"
fi

# --- mutants --------------------------------------------------------------------
mutant() { # mutant TAG - copy the gate, delete the tagged line(s), expect red
  local tag="$1" dir="${tmp}/mut-$1"
  mkdir -p "${dir}/scripts"
  cp "${repo_root}/scripts/verify-npm-audit.sh" "${dir}/scripts/"
  sed "/gate:${tag}/d" "${repo_root}/scripts/npm-audit-gate.mjs" >"${dir}/scripts/npm-audit-gate.mjs"
  if cmp -s "${repo_root}/scripts/npm-audit-gate.mjs" "${dir}/scripts/npm-audit-gate.mjs"; then
    fails=$((fails + 1)); echo "FAIL mutant ${tag}: no 'gate:${tag}' line found to remove"
    return 0
  fi
  local before="${fails}"
  verbose=0
  suite "${dir}/scripts/verify-npm-audit.sh"
  extra_suite "${dir}/scripts/verify-npm-audit.sh"
  verbose=1
  local killed=$((fails - before))
  fails="${before}"
  if [[ "${killed}" -gt 0 ]]; then
    echo "ok   mutant ${tag} killed (${killed} case(s) red)"
  else
    fails=$((fails + 1)); echo "FAIL mutant ${tag} survived: every case still green"
  fi
}
mutant expiry
mutant prod-clean

if [[ "${fails}" -ne 0 ]]; then
  echo "test-verify-npm-audit: ${fails} failure(s)" >&2
  exit 1
fi
echo "test-verify-npm-audit: all cases passed"
