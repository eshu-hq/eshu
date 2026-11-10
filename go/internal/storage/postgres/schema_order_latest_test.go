// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

var orderedBootstrapDefinitionNamesLatest = []string{
	// Migration 291 (#9975) adds the five-expression partial index the
	// keyed winners resweep probe matches per dirty identifier.
	"fact_records_supply_chain_impact_winners_keyed_idx",
	// Migration 292 (#10512) adds the generation index backing the
	// retention row-count arm and intents DELETE.
	"shared_projection_intents_generation_idx",
}
