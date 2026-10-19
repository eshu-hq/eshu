// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package producer is the Postgres implementation of
// servicecatalog.RuntimeProducerReadiness (#7816).
//
// The service catalog correlation handler reads a correlated repository's
// WorkloadInstance nodes and RUNS_ON platforms to build the runtime evidence
// family. Those nodes are written by the repository's workload_materialization
// reducer work. [Store] answers, per repository, how many of that work's
// fact_work_items rows at the repository scope's active generation are
// pending, retrying, claimed, or running, so the handler defers instead of
// committing a runtime family read before the write. It also returns the
// newest in-flight last-workable time over those rows, the latest of each
// row's cycle start, last claim, scheduled visibility, and lease expiry
// (claim_until, non-NULL only on claimed and running rows), which the
// handler's 30-minute elapsed bound is measured from, and the newest
// in-flight claim_until for the log line. It also counts each repository's
// pending repo_dependency RUNS_ON intents in shared_projection_intents, the
// second RUNS_ON writer, with their newest created_at (#8169). The lookup is
// one read-only statement, [RuntimeProducerAndRunsOnStatesQuery]: the
// [RuntimeProducerStatesQuery] half reaches fact_work_items only through
// fact_work_items_scope_generation_idx, and the [RunsOnIntentStatesQuery] half
// reaches shared_projection_intents through shared_projection_intents_repo_run_idx.
package producer
