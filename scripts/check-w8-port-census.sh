#!/usr/bin/env bash
# W8 census pre-lane drift gate: the port census must match CUTOVER_OWNERSHIP row
# for row (route set equality), carry non-empty C1-C5 on every row, assign every
# row a known C5 bucket (no residue bucket), and hit the reconciled counts
# 279 = 142 (125 Done + 17 family-native) + 27 tier-C + 110 to-migrate.
# Usage: bash scripts/check-w8-port-census.sh (run in the worktree root).
set -euo pipefail

CUTOVER="go/cmd/api/CUTOVER_OWNERSHIP.md"
CENSUS="docs/internal/w8-port-census.md"

cutover_routes=$(rg -o '^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+`' "$CUTOVER" | sort)
census_routes=$(rg -o '^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+`' "$CENSUS" | sort)

n_cut=$(printf '%s\n' "$cutover_routes" | wc -l)
n_cen=$(printf '%s\n' "$census_routes" | wc -l)
[ "$n_cut" -eq 279 ] || { echo "FAIL cutover_rows=$n_cut want 279"; exit 1; }
[ "$n_cen" -eq 279 ] || { echo "FAIL census_rows=$n_cen want 279"; exit 1; }
if ! diff <(printf '%s\n' "$cutover_routes") <(printf '%s\n' "$census_routes") > /tmp/w8-census-diff.txt; then
  echo "FAIL route set mismatch:"; cat /tmp/w8-census-diff.txt; exit 1
fi

# every table row: 7 columns, no empty C1..C5
bad=$(rg --count '^\| `[^`]+` \| [^|]* \| [^|]*\| *\|' "$CENSUS" || true)
[ -z "$bad" ] || { echo "FAIL empty cells in $bad rows"; exit 1; }

# C5 bucket heads restricted to the known set (suffix tags allowed after + or ()
n_c5bad=$(rg -o '\| (landed-Done|landed-family[^|]*|tierC-G4[^|]*|w7w-inflight[^|]*|relay-leg[^|]*) \|$' "$CENSUS" | wc -l)
[ "$n_c5bad" -eq 279 ] || { echo "FAIL c5_known=$n_c5bad want 279"; exit 1; }

# reconciled counts from buckets
done_n=$(rg -c '\| landed-Done \|' "$CENSUS")
fam_n=$(rg -c '\| landed-family' "$CENSUS")
tierc_n=$(rg -c '\| tierC-G4' "$CENSUS")
relay_n=$(rg -c '\| relay-leg' "$CENSUS")
w7w_n=$(rg -c '\| w7w-inflight' "$CENSUS")
[ "$done_n" -eq 125 ] || { echo "FAIL done=$done_n want 125"; exit 1; }
[ "$fam_n" -eq 17 ] || { echo "FAIL family=$fam_n want 17"; exit 1; }
[ "$tierc_n" -eq 27 ] || { echo "FAIL tierc=$tierc_n want 27"; exit 1; }
[ "$((relay_n + w7w_n))" -eq 110 ] || { echo "FAIL tomigrate=$((relay_n + w7w_n)) want 110"; exit 1; }

echo "PASS w8-port-census: rows=279 done=125 family=17 tierC=27 tomigrate=$((relay_n + w7w_n)) (relay=$relay_n w7w=$w7w_n)"
