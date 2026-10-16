// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// fenceOrderError reports whether names applies the fence triggers before
// the version-0 seed, and both after the migration 170 tables. The seed
// must follow the triggers: every write after the trigger commit bumps, so a
// row the seed skips (ON CONFLICT DO NOTHING) is already fenced. A seed that
// ran first would leave a window where a write changes content without a
// bump.
func fenceOrderError(names []string) error {
	index := map[string]int{}
	for i, name := range names {
		index[name] = i
	}
	tables, okTables := index["config_artifact_read_model"]
	triggers, okTriggers := index["content_files_version_fence"]
	seed, okSeed := index["config_artifact_content_versions_seed"]
	switch {
	case !okTables || !okTriggers || !okSeed:
		return fmt.Errorf("missing a PR3b migration: tables %v, triggers %v, seed %v", okTables, okTriggers, okSeed)
	case tables > triggers:
		return fmt.Errorf("fence triggers (position %d) apply before the migration 170 tables (position %d)", triggers, tables)
	case triggers > seed:
		return fmt.Errorf("version-0 seed (position %d) applies before the fence triggers (position %d)", seed, triggers)
	}
	return nil
}

// TestFenceMigrationsApplyTriggersBeforeSeed pins the PR3b-1b order in the
// shipped bootstrap definitions: 170 tables, then 184 triggers, then 185 seed.
func TestFenceMigrationsApplyTriggersBeforeSeed(t *testing.T) {
	var names []string
	for _, def := range migrations.BootstrapDefinitions() {
		names = append(names, def.Name)
	}
	if err := fenceOrderError(names); err != nil {
		t.Fatal(err)
	}
}

// TestFenceOrderErrorRejectsASeedBeforeTheTriggers is the seeded RED for the
// order check: a seed listed first, or a missing migration, fails it.
func TestFenceOrderErrorRejectsASeedBeforeTheTriggers(t *testing.T) {
	for name, names := range map[string][]string{
		"seed first":     {"config_artifact_read_model", "config_artifact_content_versions_seed", "content_files_version_fence"},
		"triggers first": {"content_files_version_fence", "config_artifact_read_model", "config_artifact_content_versions_seed"},
		"seed missing":   {"config_artifact_read_model", "content_files_version_fence"},
	} {
		if fenceOrderError(names) == nil {
			t.Errorf("%s: order check passed, want an error", name)
		}
	}
}
