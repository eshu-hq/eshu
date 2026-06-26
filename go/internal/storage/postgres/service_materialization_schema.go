// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Service materialization lineage schema (#1943, parent #1797). This is the
// additive foundation for service-scope changed-since deltas: a durable,
// versioned snapshot the reducer commits per service re-materialization, keyed by
// service_id, mirroring the repository-scope ingestion_scopes/scope_generations
// lineage that #1799 diffs. It does not change reducer_service_catalog_correlation
// facts.
