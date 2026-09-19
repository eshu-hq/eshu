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

// fenceGUCTest is the exact predicate every gated fence trigger uses: skip a
// session that marked itself derive-aware (inventory.WriterSessionSQL).
const fenceGUCTest = "current_setting('eshu.infra_inventory_writer', true) IS DISTINCT FROM 'derive'"

// TestInfraInventoryFenceTriggerLabels pins every label array in migration
// 109's rolling-upgrade fence (three function bodies and the DELETE trigger's
// WHEN) to inventory.Labels. A label missing from one would let an unaware
// writer change that label's content rows without marking the repository,
// and readers would trust a stale table.
func TestInfraInventoryFenceTriggerLabels(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("infra_resource_entities")
	arrays := regexp.MustCompile(`entity_type = ANY \(ARRAY\[([^\]]*)\]::text\[\]\)`).FindAllStringSubmatch(sql, -1)
	// Insert body: 1. Update body: new_rows and old_rows. Delete WHEN: 1.
	if len(arrays) != 4 {
		t.Fatalf("fence has %d entity_type label arrays, want 4", len(arrays))
	}
	want := slices.Sorted(slices.Values(inventory.Labels))
	quoted := regexp.MustCompile(`'([^']+)'`)
	for i, array := range arrays {
		var got []string
		for _, match := range quoted.FindAllStringSubmatch(array[1], -1) {
			got = append(got, match[1])
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("fence label array %d = %v, want inventory.Labels %v", i, got, want)
		}
	}
	if !strings.Contains(inventory.WriterSessionSQL, "eshu.infra_inventory_writer = 'derive'") {
		t.Fatalf("WriterSessionSQL %q does not set the value the triggers skip", inventory.WriterSessionSQL)
	}
}

// TestInfraInventoryFenceTriggerShape pins the trigger levels the fence
// ruling chose by measurement: INSERT and UPDATE are statement-level with
// transition tables, so a derive-aware session pays one WHEN evaluation per
// statement; DELETE is row-level with the session test first, so a bulk
// retention prune builds no transition tuplestore; TRUNCATE is ungated. The
// mark upsert must take the mark's row lock without rewriting it.
func TestInfraInventoryFenceTriggerShape(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("infra_resource_entities")
	statement := func(name string) string {
		t.Helper()
		start := strings.Index(sql, "CREATE TRIGGER "+name)
		if start < 0 {
			t.Fatalf("migration 109 does not create trigger %s", name)
		}
		end := strings.Index(sql[start:], "EXECUTE FUNCTION")
		if end < 0 {
			t.Fatalf("trigger %s has no EXECUTE FUNCTION", name)
		}
		return sql[start : start+end]
	}

	insert := statement("content_entities_infra_dirty_insert")
	for _, want := range []string{
		"AFTER INSERT ON content_entities", "REFERENCING NEW TABLE AS new_rows",
		"FOR EACH STATEMENT", "WHEN (" + fenceGUCTest + ")",
	} {
		if !strings.Contains(insert, want) {
			t.Fatalf("insert trigger missing %q:\n%s", want, insert)
		}
	}
	update := statement("content_entities_infra_dirty_update")
	for _, want := range []string{
		"AFTER UPDATE ON content_entities",
		"REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows", "FOR EACH STATEMENT",
		"WHEN (" + fenceGUCTest + ")",
	} {
		if !strings.Contains(update, want) {
			t.Fatalf("update trigger missing %q:\n%s", want, update)
		}
	}
	remove := statement("content_entities_infra_dirty_delete")
	if !strings.Contains(remove, "AFTER DELETE ON content_entities") || !strings.Contains(remove, "FOR EACH ROW") {
		t.Fatalf("delete trigger must be AFTER DELETE ... FOR EACH ROW:\n%s", remove)
	}
	_, when, ok := strings.Cut(remove, "WHEN (")
	if !ok || !strings.HasPrefix(strings.Join(strings.Fields(when), " "), fenceGUCTest+" AND OLD.entity_type") {
		t.Fatalf("delete trigger WHEN must test the session first, then OLD.entity_type:\n%s", when)
	}
	truncate := statement("content_entities_infra_dirty_truncate")
	if !strings.Contains(truncate, "AFTER TRUNCATE ON content_entities") || strings.Contains(truncate, "WHEN") {
		t.Fatalf("truncate trigger must be ungated AFTER TRUNCATE:\n%s", truncate)
	}

	if got := strings.Count(sql, "ON CONFLICT (repo_id) DO UPDATE SET marked_at = dirty.marked_at WHERE false"); got != 3 {
		t.Fatalf("lock-only mark upserts = %d, want 3 (insert, update, delete bodies)", got)
	}
	if strings.Contains(sql, "SET marked_at = EXCLUDED.marked_at") {
		t.Fatal("a mark upsert rewrites the row; use the lock-only DO UPDATE ... WHERE false form")
	}
	if strings.Contains(sql, "AFTER UPDATE OF") {
		t.Fatal("transition tables forbid an UPDATE OF column list")
	}
	for _, old := range []string{
		"content_entities_infra_fence_insert", "content_entities_infra_fence_update",
		"content_entities_infra_fence_delete", "mark_infra_resource_entity_dirty_repo()",
	} {
		if strings.Contains(sql, "CREATE TRIGGER "+old) || strings.Contains(sql, "FUNCTION "+old) {
			t.Fatalf("migration still creates the superseded row-level fence object %s", old)
		}
	}
}
