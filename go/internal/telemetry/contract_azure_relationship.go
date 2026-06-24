// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

const (
	// SpanReducerAzureRelationshipMaterialization wraps the Azure managedBy
	// relationship edge projection: fact load, in-memory join-index build keyed
	// by normalized ARM resource id, bounded managed_by support-state checks, and
	// the batched MATCH-MATCH-MERGE AZURE_managed_by edge write. The span carries
	// materialized vs skipped edge counts so a trace shows whether Azure
	// relationship targets degraded gracefully.
	SpanReducerAzureRelationshipMaterialization = "reducer.azure_relationship_materialization"
)
