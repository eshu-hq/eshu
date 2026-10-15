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
// committing a runtime family read before the write. The lookup is one
// read-only statement, [RuntimeProducerStatesQuery], that reaches
// fact_work_items only through fact_work_items_scope_generation_idx.
package producer
