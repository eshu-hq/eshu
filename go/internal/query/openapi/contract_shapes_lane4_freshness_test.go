// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import "testing"

func TestContractShapesLane4Freshness(t *testing.T) {
	t.Parallel()
	paths := lane4SpecPaths(t)

	t.Run("causality names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: status_freshness_causality.go projects the causality read
		// model; domain structs live in freshness/causality_report.go.
		props := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/status/freshness-causality", "get", "200"), "causality")
		lane4RequireExactKeys(t, props, "causality",
			"version", "as_of", "scoped", "state", "causes", "generations",
			"pending_projection", "recent_transitions", "active_work_source")
		lane4RequireDescribed(t, props, "causality",
			"version", "as_of", "scoped", "state", "causes", "generations",
			"pending_projection", "recent_transitions")
		// Owner: freshnessGenerationsToMap emits the five lifecycle counts.
		generations := lane4PropsOf(t, lane4Child(t, props, "generations", "causality"), "generations")
		lane4RequireExactKeys(t, generations, "causality.generations",
			"active", "pending", "completed", "superseded", "failed")
		lane4RequireDescribed(t, generations, "causality.generations",
			"active", "pending", "completed", "superseded", "failed")
		// Owner: freshnessPendingProjectionToMap emits the three owed-work
		// counters.
		pending := lane4PropsOf(t, lane4Child(t, props, "pending_projection", "causality"), "pending")
		lane4RequireExactKeys(t, pending, "causality.pending_projection",
			"outstanding", "dead_letter", "domains")
		lane4RequireDescribed(t, pending, "causality.pending_projection",
			"outstanding", "dead_letter", "domains")
		causes := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, props, "causes", "causality"), "causes"), "causes[]")
		lane4RequireKeys(t, causes, "causality.causes[]",
			"cause", "observed", "observability", "detail", "next_check")
		lane4RequireDescribed(t, causes, "causality.causes[]",
			"cause", "observed", "observability", "detail", "next_check")
		// Owner: answer/packet.go FreshnessNextCheckAsRecommendedCall emits
		// only the non-empty tool, route, reason, and params keys.
		nextCheck := lane4PropsOf(t, lane4Child(t, causes, "next_check", "cause"), "next_check")
		for key := range nextCheck {
			switch key {
			case "tool", "route", "reason", "params":
			default:
				t.Errorf("causality.causes[].next_check: phantom property %q", key)
			}
		}
		transitions := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, props, "recent_transitions", "causality"), "transitions"), "transitions[]")
		lane4RequireKeys(t, transitions, "causality.recent_transitions[]",
			"status", "trigger_kind", "freshness_hint", "observed_at",
			"superseded_at", "scope_id", "generation_id")
		lane4RequireDescribed(t, transitions, "causality.recent_transitions[]",
			"status", "trigger_kind", "freshness_hint", "observed_at",
			"superseded_at", "scope_id", "generation_id")
	})

	t.Run("delta pages name owner keys", func(t *testing.T) {
		t.Parallel()
		changed := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/freshness/changed-since", "get", "200"), "changed-since")
		lane4RequireDescribed(t, changed, "changed-since",
			"scope_id", "scope_kind", "repository", "since_generation_id",
			"since_observed_at", "current_active_generation_id",
			"current_observed_at", "sample_limit", "unavailable",
			"unavailable_reason", "since_is_delta", "current_is_delta", "categories")
		categories := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, changed, "categories", "changed"), "categories"), "categories[]")
		lane4RequireDescribed(t, categories, "changed.categories[]", "category", "counts", "samples", "truncated", "unavailable")
		counts := lane4PropsOf(t, lane4Child(t, categories, "counts", "category"), "counts")
		lane4RequireExactKeys(t, counts, "changed.counts",
			"added", "updated", "unchanged", "retired", "superseded")
		lane4RequireDescribed(t, counts, "changed.counts",
			"added", "updated", "unchanged", "retired", "superseded")
		serviceChanged := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/freshness/services/changed-since", "get", "200"), "service-changed")
		lane4RequireDescribed(t, serviceChanged, "service-changed",
			"service_id", "scope_id", "unattributed", "since_generation_id",
			"since_observed_at", "current_active_generation_id",
			"current_observed_at", "sample_limit", "unavailable", "categories")
		serviceCategories := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, serviceChanged, "categories", "service"), "categories"), "categories[]")
		serviceCounts := lane4PropsOf(t, lane4Child(t, serviceCategories, "counts", "category"), "counts")
		lane4RequireExactKeys(t, serviceCounts, "service-changed.counts",
			"added", "updated", "unchanged", "retired", "superseded")
		lane4RequireDescribed(t, serviceCounts, "service-changed.counts",
			"added", "updated", "unchanged", "retired", "superseded")
		gens := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/freshness/generations", "get", "200"), "generations")
		lane4RequireDescribed(t, gens, "generations", "generations", "count", "limit", "truncated")
		genRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, gens, "generations", "gens"), "rows"), "rows[]")
		lane4RequireDescribed(t, genRows, "generations[]",
			"scope_id", "generation_id", "scope_kind", "source_system",
			"collector_kind", "current_active_generation_id", "is_active",
			"trigger_kind", "freshness_hint", "status", "observed_at",
			"ingested_at", "activated_at", "superseded_at", "queue_status",
			"latest_failure")
		queue := lane4PropsOf(t, lane4Child(t, genRows, "queue_status", "row"), "queue_status")
		lane4RequireExactKeys(t, queue, "generations[].queue_status",
			"total", "outstanding", "in_flight", "retrying", "succeeded", "failed", "dead_letter")
		lane4RequireDescribed(t, queue, "generations[].queue_status",
			"total", "outstanding", "in_flight", "retrying", "succeeded", "failed", "dead_letter")
	})
}
