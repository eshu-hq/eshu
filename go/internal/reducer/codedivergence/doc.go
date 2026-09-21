// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codedivergence materializes drifted parallel-implementation
// findings: function bodies that were once the same and now differ (epic
// #6833, child #6837).
//
// The #6836 read surface serves exact and renamed equality findings by
// grouping fingerprint columns on read. Drifted (Jaccard) pairs are the most
// expensive finding, so this domain materializes them per repo generation
// instead: the handler generates candidate pairs from the LSH band side
// table (code_fingerprint_band), verifies each by exact Jaccard over the
// persisted shingle sets (code_function_fingerprint.shingles, migration
// 112), and the writer publishes admitted pairs as durable
// reducer_code_drifted_finding facts.
//
// Scaling bounds come from the #6834 theory proof
// (docs/internal/evidence/6834-code-divergence-theory.md §5–§6): ship at
// Jaccard ≥ 0.7 ([DriftedSimilarityThreshold]), at most
// [MaxCandidatesPerEntity] (200) verifications per entity ordered by
// shared-band count desc, partitioned by repo_id. Budget exhaustions are
// counted in telemetry, never silently dropped. Findings from superseded
// generations are retired with the generation; the read path serves the
// active generation only.
//
// Every finding carries truth level "derived", never "exact", with
// per-rule suppression counts; a suppressed pair is counted, never
// silently dropped.
package codedivergence
