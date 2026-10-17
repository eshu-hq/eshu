// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// fenceMigrationName is the bootstrap definition name of migration 186.
const fenceMigrationName = "content_files_version_fence"

// createTriggerPattern matches every CREATE [OR REPLACE] [CONSTRAINT] TRIGGER
// statement and captures the trigger name and the table after ON. The event
// list between them is one or more AFTER/BEFORE events joined by OR.
var createTriggerPattern = regexp.MustCompile(
	`(?is)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\s+([a-z_][a-z0-9_]*)\s+` +
		`(?:AFTER|BEFORE|INSTEAD\s+OF)\s+.*?\bON\s+([a-z_][a-z0-9_.]*)`)

// migrationFenceTriggers returns every (table, trigger) pair a migration's SQL
// creates, sorted. It strips -- comments first (sqlLineComment, schema_test.go)
// so a trigger named in prose does not count.
func migrationFenceTriggers(sql string) []FenceTrigger {
	var out []FenceTrigger
	for _, match := range createTriggerPattern.FindAllStringSubmatch(sqlLineComment.ReplaceAllString(sql, ""), -1) {
		out = append(out, FenceTrigger{Table: strings.ToLower(match[2]), Name: strings.ToLower(match[1])})
	}
	slices.SortFunc(out, compareFenceTriggers)
	return out
}

func compareFenceTriggers(a, b FenceTrigger) int {
	if c := strings.Compare(a.Table, b.Table); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}

// fenceTriggerDivergence reports how the triggers sql creates differ from
// want, or nil when they are the same set.
func fenceTriggerDivergence(sql string, want []FenceTrigger) error {
	got := migrationFenceTriggers(sql)
	sorted := slices.Clone(want)
	slices.SortFunc(sorted, compareFenceTriggers)
	if len(got) == 0 {
		return fmt.Errorf("the migration creates no trigger; want %v", sorted)
	}
	if !slices.Equal(got, sorted) {
		return fmt.Errorf("the migration creates %v; CheckFence checks %v", got, sorted)
	}
	return nil
}

// TestFenceTriggersMatchMigration186 reads the embedded migration 186, not a
// copy of its names: a renamed, added or dropped fence trigger there fails
// here until fenceTriggers changes with it.
func TestFenceTriggersMatchMigration186(t *testing.T) {
	t.Parallel()

	var sql string
	for _, def := range migrations.BootstrapDefinitions() {
		if def.Name == fenceMigrationName {
			sql = def.SQL
		}
	}
	if sql == "" {
		t.Fatalf("bootstrap definition %q not found", fenceMigrationName)
	}
	if err := fenceTriggerDivergence(sql, fenceTriggers); err != nil {
		t.Fatal(err)
	}
}

// TestFenceTriggerDivergenceRejectsAPlantedMismatch is the seeded RED for the
// migration guard: a renamed, extra, wrong-table or missing trigger fails it,
// a commented-out trigger does not count, and the matching set passes.
func TestFenceTriggerDivergenceRejectsAPlantedMismatch(t *testing.T) {
	t.Parallel()

	want := []FenceTrigger{
		{"content_files", "content_files_version_fence_insert"},
		{"config_artifact_fence_pending", "config_artifact_fence_pending_commit"},
	}
	insert := "CREATE OR REPLACE TRIGGER content_files_version_fence_insert\n    AFTER INSERT ON content_files\n" +
		"    REFERENCING NEW TABLE AS fence_new_rows FOR EACH STATEMENT EXECUTE FUNCTION f();\n"
	commit := "        CREATE CONSTRAINT TRIGGER config_artifact_fence_pending_commit\n" +
		"            AFTER INSERT ON config_artifact_fence_pending\n" +
		"            DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION g();\n"

	if err := fenceTriggerDivergence(insert+commit, want); err != nil {
		t.Fatalf("matching set: %v", err)
	}
	for name, sql := range map[string]string{
		"renamed": strings.Replace(insert, "fence_insert", "fence_ins", 1) + commit,
		"extra": insert + commit +
			"CREATE TRIGGER content_files_version_fence_extra AFTER UPDATE OR DELETE ON content_files EXECUTE FUNCTION f();\n",
		"wrong table": strings.Replace(insert, "ON content_files", "ON content_entities", 1) + commit,
		"missing":     insert,
		"commented":   insert + "-- " + strings.ReplaceAll(strings.TrimSpace(commit), "\n", " ") + "\n",
		"empty":       "SELECT 1;",
	} {
		if fenceTriggerDivergence(sql, want) == nil {
			t.Errorf("%s: divergence check passed, want an error", name)
		}
	}
}
