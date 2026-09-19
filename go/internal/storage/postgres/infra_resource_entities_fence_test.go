// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestInfraInventoryFenceTriggerLabels pins every label list in migration
// 109's rolling-upgrade fence triggers to inventory.Labels. A label missing
// from a trigger would let an unaware writer change that label's content rows
// without marking the repository, and readers would trust a stale table.
func TestInfraInventoryFenceTriggerLabels(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("infra_resource_entities")
	lists := regexp.MustCompile(`entity_type IN \(([^)]*)\)`).FindAllStringSubmatch(sql, -1)
	// Insert and delete each have one list; update checks NEW and OLD.
	if len(lists) != 4 {
		t.Fatalf("fence triggers have %d entity_type label lists, want 4", len(lists))
	}
	want := slices.Sorted(slices.Values(inventory.Labels))
	quoted := regexp.MustCompile(`'([^']+)'`)
	for i, list := range lists {
		var got []string
		for _, match := range quoted.FindAllStringSubmatch(list[1], -1) {
			got = append(got, match[1])
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("fence trigger label list %d = %v, want inventory.Labels %v", i, got, want)
		}
	}
	for _, trigger := range []string{
		"content_entities_infra_fence_insert",
		"content_entities_infra_fence_update",
		"content_entities_infra_fence_delete",
	} {
		if !strings.Contains(sql, "CREATE TRIGGER "+trigger) {
			t.Fatalf("migration 109 does not create trigger %s", trigger)
		}
	}
	if got := strings.Count(sql, "current_setting('eshu.infra_inventory_writer', true) IS DISTINCT FROM 'derive'"); got != 3 {
		t.Fatalf("fence triggers skip derive-aware writers in %d of 3 WHEN clauses", got)
	}
	if !strings.Contains(inventory.WriterSessionSQL, "eshu.infra_inventory_writer = 'derive'") {
		t.Fatalf("WriterSessionSQL %q does not set the value the triggers skip", inventory.WriterSessionSQL)
	}
	if strings.Contains(sql, "DO NOTHING") {
		t.Fatal("fence upsert must lock the mark (DO UPDATE) so a repair waits for an open unaware write")
	}
}
