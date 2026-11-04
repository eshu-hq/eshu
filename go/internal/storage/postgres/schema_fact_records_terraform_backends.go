// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// terraformBackendsFactRecordReadIndexesSQL serves the backend_generations
// CTE of the canonical terraform-backends reader
// (backend_canonical.go) and the
// matching_backend_generations CTE of the backend-filter reader
// (tfstate/backend_queries.go). The CASE predicate is byte-aligned with
// those queries' load-bearing array-length filter (modulo table alias) so
// the planner can prove the implication; keep the three in lockstep. The key
// covers the CTE's (scope_id, generation_id, repo_id) output for an
// index-only probe. No is_tombstone term: the served query does not filter
// tombstones, and an extra predicate term would make the index unusable.
const terraformBackendsFactRecordReadIndexesSQL = `
CREATE INDEX IF NOT EXISTS fact_records_terraform_backends_active_idx
    ON fact_records (scope_id, generation_id, (payload->>'repo_id'))
    WHERE fact_kind = 'file'
      AND source_system = 'git'
      AND CASE
            WHEN jsonb_typeof(payload->'parsed_file_data'->'terraform_backends') = 'array'
            THEN jsonb_array_length(payload->'parsed_file_data'->'terraform_backends')
            ELSE 0
          END > 0;
`
