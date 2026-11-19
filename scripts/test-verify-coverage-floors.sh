#!/usr/bin/env bash
# Self-test for scripts/verify-coverage-floors.sh (Refs eshu-hq/eshu#10983).
# Hermetic RED/GREEN mirror: fixture profiles only, no Go build, no network,
# no Postgres, no Docker.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-coverage-floors.sh"
floors="${repo_root}/specs/coverage-floors.v1.yaml"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

if [[ -f "${verifier}" && -x "${verifier}" ]]; then
	record_pass "verifier exists and is executable"
else
	record_fail "verifier must exist and be executable"
fi
bash -n "${verifier}" 2>/dev/null && record_pass "verifier has valid shell syntax" || record_fail "verifier shell syntax is invalid"

# Fixture floors: whole-module floor 74, one env_gated entry, three packages.
fixture_floors="${tmp_root}/floors.yaml"
cat >"${fixture_floors}" <<'YAML'
schema_version: 1
whole_module_floor: 74
env_gated:
  - example.com/go/gated
packages:
  example.com/go/alpha: 80
  example.com/go/beta: 50
  example.com/go/gamma: 0
YAML

# Fixture profile: alpha 8/10 (80%), beta 1/4 (25%), gamma 0/2 (0%),
# newpkg 4/4 (100%, no floor entry). Whole module: 13/20 = 65% canonical.
low_profile="${tmp_root}/low.out"
cat >"${low_profile}" <<'PROFILE'
mode: count
example.com/go/alpha/a.go:1.1,2.1 8 1
example.com/go/alpha/a.go:3.1,4.1 2 0
example.com/go/beta/b.go:1.1,2.1 1 1
example.com/go/beta/b.go:3.1,4.1 3 0
example.com/go/gamma/g.go:1.1,2.1 2 0
example.com/go/newpkg/n.go:1.1,2.1 4 1
PROFILE

# High profile: alpha 9/10 (90%), beta 3/4 (75%), gamma 2/2 (100%).
# Whole module: 14/16 = 87% canonical.
high_profile="${tmp_root}/high.out"
cat >"${high_profile}" <<'PROFILE'
mode: count
example.com/go/alpha/a.go:1.1,2.1 9 1
example.com/go/alpha/a.go:3.1,4.1 1 0
example.com/go/beta/b.go:1.1,2.1 3 1
example.com/go/beta/b.go:3.1,4.1 1 0
example.com/go/gamma/g.go:1.1,2.1 2 1
PROFILE

run_verifier() {
	local status=0
	"$@" >"${tmp_root}/run.log" 2>&1 || status=$?
	printf '%d' "${status}"
}

# RED: whole-module 65% below floor 74.
status="$(run_verifier "${verifier}" --profile "${low_profile}" --floors "${fixture_floors}")"
if [[ "${status}" -eq 1 ]]; then
	record_pass "whole-module below floor exits 1"
else
	record_fail "whole-module below floor exits ${status}, want 1"
fi
if rg --fixed-strings --quiet -- "whole-module 65% below floor 74%" "${tmp_root}/run.log"; then
	record_pass "whole-module failure names the measured and floor values"
else
	record_fail "whole-module failure must name measured and floor values"
fi

# GREEN: whole-module 87% above floor 74, with headroom.
status="$(run_verifier "${verifier}" --profile "${high_profile}" --floors "${fixture_floors}")"
if [[ "${status}" -eq 0 ]]; then
	record_pass "whole-module above floor exits 0"
else
	record_fail "whole-module above floor exits ${status}, want 0"
fi
if rg --fixed-strings --quiet -- "headroom +13" "${tmp_root}/run.log"; then
	record_pass "whole-module pass reports rising-floor headroom"
else
	record_fail "whole-module pass must report headroom"
fi

# RED: changed package beta 25% below floor 50%.
status="$(run_verifier "${verifier}" --profile "${low_profile}" --floors "${fixture_floors}" --changed example.com/go/beta)"
if [[ "${status}" -eq 1 ]]; then
	record_pass "changed package below floor exits 1"
else
	record_fail "changed package below floor exits ${status}, want 1"
fi
if rg --fixed-strings --quiet -- "example.com/go/beta" "${tmp_root}/run.log"; then
	record_pass "package failure names the package"
else
	record_fail "package failure must name the package"
fi

# GREEN: changed package alpha exactly at floor 80%.
status="$(run_verifier "${verifier}" --profile "${low_profile}" --floors "${fixture_floors}" --changed example.com/go/alpha)"
if [[ "${status}" -eq 0 ]]; then
	record_pass "changed package at floor exits 0"
else
	record_fail "changed package at floor exits ${status}, want 0"
fi

# Mixed csv: one passing, one failing still fails.
status="$(run_verifier "${verifier}" --profile "${low_profile}" --floors "${fixture_floors}" --changed example.com/go/alpha,example.com/go/beta)"
if [[ "${status}" -eq 1 ]]; then
	record_pass "mixed changed set fails on the regressed package"
else
	record_fail "mixed changed set exits ${status}, want 1"
fi

# Unknown changed package is record-only.
status="$(run_verifier "${verifier}" --profile "${low_profile}" --floors "${fixture_floors}" --changed example.com/go/newpkg)"
if [[ "${status}" -eq 0 ]]; then
	record_pass "changed package without a floor entry passes record-only"
else
	record_fail "unknown changed package exits ${status}, want 0"
fi
if rg --fixed-strings --quiet -- "NOTICE example.com/go/newpkg" "${tmp_root}/run.log"; then
	record_pass "record-only pass is announced as NOTICE"
else
	record_fail "record-only pass must print NOTICE"
fi

# env_gated changed package is skipped even below any floor.
gated_profile="${tmp_root}/gated.out"
cat >"${gated_profile}" <<'PROFILE'
mode: count
example.com/go/gated/g.go:1.1,2.1 10 0
PROFILE
status="$(run_verifier "${verifier}" --profile "${gated_profile}" --floors "${fixture_floors}" --changed example.com/go/gated)"
if [[ "${status}" -eq 0 ]]; then
	record_pass "env_gated changed package is skipped hermetically"
else
	record_fail "env_gated changed package exits ${status}, want 0"
fi
if rg --fixed-strings --quiet -- "SKIP example.com/go/gated is env_gated" "${tmp_root}/run.log"; then
	record_pass "env_gated skip names the package"
else
	record_fail "env_gated skip must name the package"
fi

# Changed package absent from the profile is skipped.
status="$(run_verifier "${verifier}" --profile "${low_profile}" --floors "${fixture_floors}" --changed example.com/go/alpha,example.com/go/empty)"
if [[ "${status}" -eq 0 ]]; then
	record_pass "changed package absent from the profile is skipped"
else
	record_fail "absent changed package exits ${status}, want 0"
fi

# Ranking: byte-exact ascending order (the low fixture sits below the
# whole-module floor, so the floor exit is irrelevant here).
ranking="${tmp_root}/ranking.tsv"
"${verifier}" --profile "${low_profile}" --floors "${fixture_floors}" --ranking-out "${ranking}" >/dev/null 2>&1 || true
expected="${tmp_root}/ranking.want"
{
	printf '0\t2\t0\texample.com/go/gamma\n'
	printf '25\t4\t1\texample.com/go/beta\n'
	printf '80\t10\t8\texample.com/go/alpha\n'
	printf '100\t4\t4\texample.com/go/newpkg\n'
} >"${expected}"
if cmp -s "${ranking}" "${expected}"; then
	record_pass "ranking renders byte-identically in ascending order"
else
	record_fail "ranking must render byte-identically ascending"
fi

# Exclusion parity with the generator: testdata and generated rows excluded.
excl_profile="${tmp_root}/excl.out"
cat >"${excl_profile}" <<'PROFILE'
mode: count
example.com/go/alpha/a.go:1.1,2.1 8 1
example.com/go/alpha/a.go:3.1,4.1 2 0
example.com/go/alpha/testdata/fixture.go:1.1,2.1 100 0
example.com/go/alpha/service.pb.go:1.1,2.1 100 0
PROFILE
status="$(run_verifier "${verifier}" --profile "${excl_profile}" --floors "${fixture_floors}" --changed example.com/go/alpha)"
if [[ "${status}" -eq 0 ]]; then
	record_pass "excluded rows do not dilute package coverage"
else
	record_fail "excluded rows must not dilute package coverage (exit ${status})"
fi

# Usage error.
status="$(run_verifier "${verifier}" --floors "${fixture_floors}")"
if [[ "${status}" -eq 2 ]]; then
	record_pass "missing profile exits 2"
else
	record_fail "missing profile exits ${status}, want 2"
fi

# Malformed floors file fails visibly.
bad_floors="${tmp_root}/bad.yaml"
printf 'schema_version: 1\npackages:\n  - not-a-mapping\n' >"${bad_floors}"
status="$(run_verifier "${verifier}" --profile "${low_profile}" --floors "${bad_floors}")"
if [[ "${status}" -eq 2 ]]; then
	record_pass "malformed floors file exits 2"
else
	record_fail "malformed floors file exits ${status}, want 2"
fi

# The committed floors file loads: shape, scale, and policy values.
if [[ -f "${floors}" ]]; then
	record_pass "committed floors file exists"
else
	record_fail "committed floors file is missing"
fi
if rg --fixed-strings --quiet -- "whole_module_floor: 74" "${floors}"; then
	record_pass "committed whole-module floor matches the published baseline"
else
	record_fail "committed whole-module floor must be 74"
fi
if [[ "$(rg --count -- '^  - ' "${floors}")" -eq 6 ]]; then
	record_pass "committed env_gated list names the six failing packages"
else
	record_fail "committed env_gated list must hold six entries"
fi
if [[ "$(rg --count -- '^  github\.com/eshu-hq/eshu/go/[^:]+: [0-9]+$' "${floors}")" -ge 1000 ]]; then
	record_pass "committed per-package floors cover the measured module"
else
	record_fail "committed per-package floors must cover the measured module"
fi
status="$(run_verifier "${verifier}" --profile "${low_profile}" --floors "${floors}" --changed example.com/go/newpkg)"
if [[ "${status}" -eq 0 ]]; then
	record_pass "committed floors file parses under the verifier"
else
	record_fail "committed floors file must parse (exit ${status})"
fi
if [[ "$(wc -l <"${floors}")" -lt 500 ]]; then
	printf 'note - floors file is small enough to eyeball\n'
else
	printf 'note - floors file is generated data (precedent: replay-coverage-manifest.v1.yaml)\n'
fi

if [[ "${FAIL}" -ne 0 ]]; then
	printf 'verify-coverage-floors tests FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
	exit 1
fi

printf 'verify-coverage-floors tests passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
