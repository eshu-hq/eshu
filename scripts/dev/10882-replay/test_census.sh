#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Seeded RED/GREEN proof for census_not_takeable.sql (#11187). GREEN: with one
# seeded row of each not-takeable class the census reports exactly one row in
# each. RED: a mutated copy of the census (each class's CASE arm turned off in
# turn) must fail the same check, so the check is not satisfied by accident.
#
# Usage: test_census.sh <dsn of a DISPOSABLE database with the Eshu schema>
set -euo pipefail

dsn="${1:?usage: test_census.sh <disposable dsn>}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
want="conflict_fenced|1
projector_drain_blocked|1
readiness_gated|1
stale_generation_fenced|1
takeable|1
timer_future_visible|1
woken_holding_wait|1
parked_wait_unwoken|1"

psql "$dsn" -X -q -v ON_ERROR_STOP=1 -f "$here/census_seed_violations.sql" >/dev/null

# census.sh prints psql's aligned table; reduce it to class|row_count lines.
psql_tuples() {
  awk -F'|' 'NF>=5 && $1 !~ /class|---/ {gsub(/ /,"",$1); gsub(/ /,"",$2); print $1"|"$2}' | sort
}

expected="$(printf '%s\n' "$want" | sort)"
actual="$(CENSUS_SQL="$here/census_not_takeable.sql" "$here/census.sh" "$dsn" census-src 3600 | psql_tuples)"
if [[ "$actual" != "$expected" ]]; then
  echo "GREEN FAILED: census classes differ from the seed" >&2
  diff <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >&2 || true
  exit 1
fi
echo "GREEN: all 8 classes seeded and found"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail=0
for arm in stale_generation_fenced parked_wait_unwoken woken_holding_wait timer_future_visible \
  readiness_gated conflict_fenced projector_drain_blocked; do
  # Turn the arm's WHEN into WHEN FALSE: its rows then fall to a later class.
  sed "s/WHEN \(.*\) THEN '$arm'/WHEN FALSE THEN '$arm'/" "$here/census_not_takeable.sql" >"$tmp/mutant.sql"
  if cmp -s "$here/census_not_takeable.sql" "$tmp/mutant.sql"; then
    echo "RED SETUP FAILED: mutation of $arm changed nothing" >&2
    fail=1
    continue
  fi
  got="$(CENSUS_SQL="$tmp/mutant.sql" "$here/census.sh" "$dsn" census-src 3600 | psql_tuples)"
  if [[ "$got" == "$expected" ]]; then
    echo "RED FAILED: mutating $arm still passed" >&2
    fail=1
  else
    echo "RED ok: mutating $arm is caught"
  fi
done
exit "$fail"
