#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Runs the #11187 not-takeable census (census_not_takeable.sql) for one source
# against one database. The reducer claim's readiness requirements are not a
# table: they live as a VALUES list in Go. This script lifts that list from the
# source tree of the commit under test into a session temp table, so the census
# can never carry a stale copy of it.
#
# Usage: census.sh <dsn> <source_system> <engage_age_seconds> [repo_root]
set -euo pipefail

dsn="${1:?usage: census.sh <dsn> <source_system> <engage_age_seconds> [repo_root]}"
source_system="${2:?source_system required}"
engage_age_seconds="${3:?engage_age_seconds required}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="${4:-$(cd "$here/../../.." && pwd)}"
go_file="$repo_root/go/internal/storage/postgres/reducer_queue_readiness_sql.go"

rows="$(sed -n '/^func reducerClaimReadinessRequirementsSQL/,/^}/p' "$go_file" |
  rg "^\s+\('" | sed -e 's/^[[:space:]]*//' -e 's/,$//' -e 's/`$//' | paste -sd, -)"
if [[ -z "$rows" ]]; then
  echo "census.sh: no readiness requirement rows found in $go_file" >&2
  exit 2
fi

psql "$dsn" -X -q -v ON_ERROR_STOP=1 \
  -v source="$source_system" -v engage_age_seconds="$engage_age_seconds" <<SQL
CREATE TEMP TABLE reducer_claim_readiness_requirements AS
SELECT * FROM (VALUES $rows)
  AS v(domain, keyspace, phase, acceptance_unit_source, acceptance_unit_prefix);
\i ${CENSUS_SQL:-$here/census_not_takeable.sql}
SQL
