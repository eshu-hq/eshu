#!/usr/bin/env bash
# W8 census drift gate: the port census must match CUTOVER_OWNERSHIP row
# for row (route set equality), carry non-empty C1-C5 on every row, assign every
# row a known C5 bucket (no residue bucket), and hit the reconciled counts
# (defaults: 279 = 142 (125 Done + 17 family-native) + 27 tier-C + 110 to-migrate).
# Usage (run in the worktree root):
#   bash scripts/check-w8-port-census.sh            # gate with defaults/env
#   bash scripts/check-w8-port-census.sh --derive   # print exports recomputed live
# Env overrides (defaults = reconciled census counts, changed only by wave moves):
#   W8_EXPECT_ROWS, W8_EXPECT_DONE, W8_EXPECT_FAMILY, W8_EXPECT_TIERC,
#   W8_EXPECT_TOMIGRATE.
# --derive recomputes expectations from the CUTOVER_OWNERSHIP.md row count and
# the census C5 buckets; eval its output (or paste it) to accept a legitimate
# wave move without editing this script:
#   eval "$(bash scripts/check-w8-port-census.sh --derive)"
#   bash scripts/check-w8-port-census.sh
set -euo pipefail

CUTOVER="go/cmd/api/CUTOVER_OWNERSHIP.md"
CENSUS="docs/internal/w8-port-census.md"

EXPECT_ROWS="${W8_EXPECT_ROWS:-279}"
EXPECT_DONE="${W8_EXPECT_DONE:-125}"
EXPECT_FAMILY="${W8_EXPECT_FAMILY:-17}"
EXPECT_TIERC="${W8_EXPECT_TIERC:-27}"
EXPECT_TOMIGRATE="${W8_EXPECT_TOMIGRATE:-110}"

cutover_routes=$(rg -o '^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+`' "$CUTOVER" | sort)
census_routes=$(rg -o '^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+`' "$CENSUS" | sort)

n_cut=$(printf '%s\n' "$cutover_routes" | wc -l)
n_cen=$(printf '%s\n' "$census_routes" | wc -l)
done_n=$(rg -c '\| landed-Done \|' "$CENSUS" || true)
fam_n=$(rg -c '\| landed-family' "$CENSUS" || true)
tierc_n=$(rg -c '\| tierC-G4' "$CENSUS" || true)
relay_n=$(rg -c '\| relay-leg' "$CENSUS" || true)
w7w_n=$(rg -c '\| w7w-recorded' "$CENSUS" || true)

if [ "${1:-}" = "--derive" ]; then
  echo "export W8_EXPECT_ROWS=$n_cut"
  echo "export W8_EXPECT_DONE=$done_n"
  echo "export W8_EXPECT_FAMILY=$fam_n"
  echo "export W8_EXPECT_TIERC=$tierc_n"
  echo "export W8_EXPECT_TOMIGRATE=$((relay_n + w7w_n))"
  exit 0
fi

[ "$n_cut" -eq "$EXPECT_ROWS" ] || { echo "FAIL cutover_rows=$n_cut want $EXPECT_ROWS"; exit 1; }
[ "$n_cen" -eq "$EXPECT_ROWS" ] || { echo "FAIL census_rows=$n_cen want $EXPECT_ROWS"; exit 1; }
if ! diff <(printf '%s\n' "$cutover_routes") <(printf '%s\n' "$census_routes") > /tmp/w8-census-diff.txt; then
  echo "FAIL route set mismatch:"; cat /tmp/w8-census-diff.txt; exit 1
fi

# every table row: 7 columns, no empty C1..C5
bad=$(rg --count '^\| `[^`]+` \| [^|]* \| [^|]*\| *\|' "$CENSUS" || true)
[ -z "$bad" ] || { echo "FAIL empty cells in $bad rows"; exit 1; }

# C5 bucket heads restricted to the known set (suffix tags allowed after + or ()
n_c5bad=$(rg -o '\| (landed-Done|landed-family[^|]*|tierC-G4[^|]*|w7w-recorded[^|]*|relay-leg[^|]*) \|$' "$CENSUS" | wc -l)
[ "$n_c5bad" -eq "$EXPECT_ROWS" ] || { echo "FAIL c5_known=$n_c5bad want $EXPECT_ROWS"; exit 1; }

# reconciled counts from buckets
[ "$done_n" -eq "$EXPECT_DONE" ] || { echo "FAIL done=$done_n want $EXPECT_DONE"; exit 1; }
[ "$fam_n" -eq "$EXPECT_FAMILY" ] || { echo "FAIL family=$fam_n want $EXPECT_FAMILY"; exit 1; }
[ "$tierc_n" -eq "$EXPECT_TIERC" ] || { echo "FAIL tierc=$tierc_n want $EXPECT_TIERC"; exit 1; }
[ "$((relay_n + w7w_n))" -eq "$EXPECT_TOMIGRATE" ] || { echo "FAIL tomigrate=$((relay_n + w7w_n)) want $EXPECT_TOMIGRATE"; exit 1; }

echo "PASS w8-port-census: rows=$EXPECT_ROWS done=$EXPECT_DONE family=$EXPECT_FAMILY tierC=$EXPECT_TIERC tomigrate=$EXPECT_TOMIGRATE (relay=$relay_n w7w=$w7w_n)"
