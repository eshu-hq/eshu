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
	// Migration 293 (#10544) drops the triple-only package-lookup index
	// whose rows=1 cost tie kept the planner off the 291 keyed index.
	"drop_fact_records_supply_chain_impact_package_lookup_idx",
	// Migration 294 (#10525) adds the generation index backing the
	// retention work-items count arm and pre-delete lock. 293 went to
	// #10544, which rolled first, so this lane takes 294.
	"fact_work_items_generation_idx",
	// Migration 295 (#10566) seeds the synthetic advisory-groups
	// scope/generation anchor the coalesced projection rows re-home to.
	"advisory_groups_synthetic_scope",
}
