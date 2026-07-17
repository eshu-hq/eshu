// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// codeFlowFactRecordReadIndexesSQL indexes the cumulative-active code-flow read
// (query.listActiveCodeFlowFactsSQL, serving POST /api/v0/code/flow/*): it
// filters fact_records by `fact_kind IN (code-flow kinds)` and
// `payload->>'repo_id' = $repo`, joined to the active scope generation.
//
// Without this index the planner can satisfy the fact_kind + scope_id join via
// fact_records_scope_generation_idx, but `payload->>'repo_id'` remains a
// residual heap filter: the read heap-fetches every code-flow fact in every
// scope and discards the non-target rows. Measured on a seeded corpus of
// ~110k code-flow facts, a single-repo read fetched ~91k buffers and removed
// ~108k rows by filter (~68 ms); with this partial index the same read used
// ~2.3k buffers (~8 ms), because the leading `payload->>'repo_id'` expression
// key makes the target repo directly seekable within the code-flow partition.
// The cost is otherwise linear in total corpus code-flow volume rather than the
// target repo's fact count.
//
// The partial predicate deliberately does NOT exclude tombstones: the read's
// ranked_candidates CTE must rank retracted facts alongside live ones to pick
// the newest generation per stable_fact_key, then drops rn=1 tombstones in its
// outer filter. Excluding tombstones here would let an older live fact win the
// ranking over a newer retraction and resurface deleted code-flow evidence.
const codeFlowFactRecordReadIndexesSQL = `
CREATE INDEX IF NOT EXISTS fact_records_code_flow_repo_idx ON fact_records ((payload->>'repo_id'), scope_id, generation_id, fact_id) WHERE fact_kind IN ('code_taint_evidence', 'code_interproc_evidence', 'code_dataflow_function');
`
