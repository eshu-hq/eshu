// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBootstrapDefinitionsIncludeCrossScopeCompletionQueue(t *testing.T) {
	t.Parallel()

	var def Definition
	for _, candidate := range BootstrapDefinitions() {
		if candidate.Name == "cross_scope_completion_queue" {
			def = candidate
			break
		}
	}
	if def.Name == "" {
		t.Fatal("cross_scope_completion_queue definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS cross_scope_completion_events",
		"producer_item_count BIGINT NOT NULL",
		"claim_epoch BIGINT NOT NULL DEFAULT 0",
		"cross_scope_completion_events_pending_idx",
		"cross_scope_completion_events_queued_domain_uniq",
		"cross_scope_completion_events_live_domain_uniq",
		"ADD COLUMN cross_scope_replay_required",
		"ADD COLUMN cross_scope_completion_ack_epoch",
		"BEFORE UPDATE OF status ON fact_work_items",
		"AFTER UPDATE OF status ON fact_work_items",
		"FOR EACH ROW",
		"OLD.cross_scope_completion_ack_epoch = NEW.cross_scope_completion_ack_epoch",
		"cross_scope_completion_events.created_at + INTERVAL '2 seconds'",
		"CHECK (producer_item_count > 0)",
		"cross_scope_completion_events_lease_shape_check",
	} {
		if !strings.Contains(def.SQL, want) {
			t.Fatalf("cross-scope completion schema SQL missing %q:\n%s", want, def.SQL)
		}
	}

	var indexDef Definition
	for _, candidate := range BootstrapDefinitions() {
		if candidate.Name == "fact_work_items_cross_scope_source_idx" {
			indexDef = candidate
			break
		}
	}
	if indexDef.Name == "" {
		t.Fatal("fact_work_items_cross_scope_source_idx definition missing")
	}
	if !strings.Contains(indexDef.SQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS") {
		t.Fatalf("populated fact_work_items index must build concurrently:\n%s", indexDef.SQL)
	}
	if strings.Contains(def.SQL, "fact_work_items_cross_scope_source_idx") {
		t.Fatal("concurrent fact_work_items index cannot share migration 093's multi-statement transaction")
	}
	for _, forbidden := range []string{
		"producer_work_item_ids",
		"processed_at",
		"fanout_token",
		"cross_scope_completion_event_token",
	} {
		if strings.Contains(def.SQL, forbidden) {
			t.Fatalf("bounded queue schema retains terminal/audit field %q:\n%s", forbidden, def.SQL)
		}
	}
	if strings.Contains(
		def.SQL,
		"CHECK (status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded'))",
	) {
		t.Fatalf("completion events must not retain succeeded rows:\n%s", def.SQL)
	}

	seedSQL := MigrationSQL("cross_scope_completion_upgrade_seed")
	for _, want := range []string{
		"cross_scope_completion_upgrade_095",
		"container_image_identity",
		"ci_cd_run_correlation",
		"cross_scope_completion_upgrade_markers",
		"cross_scope_replay_required = source.status IN ('claimed', 'running')",
		"WHEN source.status = 'succeeded' THEN 'pending'",
	} {
		if !strings.Contains(seedSQL, want) {
			t.Fatalf("quiet-upgrade seed missing %q:\n%s", want, seedSQL)
		}
	}
}

func TestCrossScopeCompletionSchemaCoversCatalogDomainsExactly(t *testing.T) {
	t.Parallel()
	producerSet := make(map[string]struct{})
	consumerSet := make(map[string]struct{})
	for _, edge := range reducer.CrossScopeCompletionEdges() {
		producerSet[string(edge.Producer)] = struct{}{}
		consumerSet[string(edge.Consumer)] = struct{}{}
	}
	quotedSorted := func(set map[string]struct{}) string {
		values := make([]string, 0, len(set))
		for value := range set {
			values = append(values, "'"+value+"'")
		}
		slices.Sort(values)
		return strings.Join(values, ", ")
	}
	queueSQL := MigrationSQL("cross_scope_completion_queue")
	producerList := quotedSorted(producerSet)
	for _, fragment := range []string{
		"CHECK (producer_domain IN (" + producerList + "))",
		"NEW.domain IN (" + producerList + ")",
	} {
		if !strings.Contains(queueSQL, fragment) {
			t.Fatalf("completion producer catalog drift: migration missing %q", fragment)
		}
	}
	indexSQL := MigrationSQL("fact_work_items_cross_scope_source_idx")
	consumerList := quotedSorted(consumerSet)
	if fragment := "domain IN (" + consumerList + ")"; !strings.Contains(indexSQL, fragment) {
		t.Fatalf("completion consumer catalog drift: index missing %q", fragment)
	}
}
