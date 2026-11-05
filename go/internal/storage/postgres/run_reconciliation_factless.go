// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Factless-convergence probe for #9257: a completed work item whose generation
// committed nothing for the reducer to project has no fact_records rows for
// the item's (scope, generation) and no fact_work_items rows for the
// generation. A completed claim whose result carries no facts (for example an
// unchanged-state merge) skips ingestion entirely, so neither table ever sees
// the generation; counting those items as vacuously published keeps the run
// from wedging in reducer_converging (#9257). Both NOT EXISTS probes hit
// (scope_id, generation_id) index prefixes
// (fact_records_scope_generation_idx, fact_work_items_scope_generation_idx),
// so one reconcile pass costs one cheap probe per completed item instead of a
// table scan. Split out to keep workflow_run_reconciliation.go under the
// 500-line cap.
//
// The probe is a fragment of listWorkflowCollectorProgressQuery, not its own
// statement: the reconciler reads under READ COMMITTED, where every statement
// sees a new snapshot, and a separate probe counted factless completions that
// landed after the progress statement ran. That broke the
// factless <= completed invariant ReconcileRunProgress validates and failed
// the reconcile tick under concurrent completion (#10091). Inside the
// progress statement's FILTER both counts share one snapshot, so the subset
// relation holds by construction.

// factlessCompletedProbeSQL is appended to the completed-status FILTER of
// listWorkflowCollectorProgressQuery; the alias `item` must name
// workflow_work_items there.
const factlessCompletedProbeSQL = `
      AND NOT EXISTS (
        SELECT 1
        FROM fact_records AS fact
        WHERE fact.scope_id = item.scope_id
          AND fact.generation_id = item.generation_id
      )
      AND NOT EXISTS (
        SELECT 1
        FROM fact_work_items AS work
        WHERE work.scope_id = item.scope_id
          AND work.generation_id = item.generation_id
      )`
