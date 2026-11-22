#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Derivable code census for #10881 (writer sites, reader shapes). Every
# figure in the design record comes from a command in this file; run it at
# the commit you cite and diff against the record.
# Usage: bash census_code.sh   (inside a checkout; uses git grep)
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
G=(git grep -n)
NT=(-- 'go/*.go' ':!*_test.go')

echo "## fact_records writer sites: INSERT INTO fact_records, production Go"
"${G[@]}" 'INSERT INTO fact_records' "${NT[@]}" ':!go/internal/storage/postgres/testfixtures/*' | cut -d: -f1,2
echo "count=$("${G[@]}" 'INSERT INTO fact_records' "${NT[@]}" ':!go/internal/storage/postgres/testfixtures/*' | wc -l)"

echo
echo "## ON CONFLICT (fact_id) as SQL (comment lines excluded), production Go"
"${G[@]}" -E '^ON CONFLICT \(fact_id\)' "${NT[@]}" | cut -d: -f1,2
echo "note: relationship_reference_keys.go targets relationship_reference_candidate_keys, not fact_records"

echo
echo "## JOIN fact_records: non-test Go lines / files, then incl. tests"
echo "lines=$("${G[@]}" 'JOIN fact_records' "${NT[@]}" | wc -l) files=$(git grep -l 'JOIN fact_records' "${NT[@]}" | wc -l)"
echo "lines_with_tests=$("${G[@]}" 'JOIN fact_records' -- 'go/*.go' | wc -l) files_with_tests=$(git grep -l 'JOIN fact_records' -- 'go/*.go' | wc -l)"

echo
echo "## fact_id-only lookups, joins and deletes (no scope_id beside fact_id)"
"${G[@]}" -E 'fact\.fact_id = ANY|f\.fact_id = v\.fact_id|(ref|consumption|owner)\.fact_id = |fact\.fact_id = (ref|consumption|owner|member)\.|^WHERE fact_id = ANY|f\.fact_id = w\.' "${NT[@]}" | cut -c1-150

echo
echo "## DELETE FROM fact_records sites"
"${G[@]}" 'DELETE FROM fact_records' "${NT[@]}" | cut -d: -f1,2

echo
echo "## generation_id-only predicates on fact rows (no scope prune)"
"${G[@]}" -E '(fact|f)\.generation_id = ANY' "${NT[@]}" | cut -c1-150
