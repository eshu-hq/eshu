// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestCodeReachabilitySchemaSQLHasVerdictTable(t *testing.T) {
	sqlStr := CodeReachabilitySchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS code_root_verdicts",
		"PRIMARY KEY (scope_id, generation_id, repository_id, entity_id, root_kind)",
		"code_root_verdicts_repo_entity_verdict_idx",
		// #5376 P1 upgrade-backfill (Option C): the epoch column and its
		// idempotent ADD COLUMN IF NOT EXISTS.
		"ADD COLUMN IF NOT EXISTS verdict_schema_epoch INTEGER NOT NULL DEFAULT 0",
	} {
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("CodeReachabilitySchemaSQL() missing %q:\n%s", want, sqlStr)
		}
	}
}

func TestCodeReachabilityRootsQueryLoadsClassContext(t *testing.T) {
	for _, want := range []string{
		"metadata->>'class_context' AS class_context",
		"metadata->'dead_code_root_kinds' AS root_kinds",
	} {
		if !strings.Contains(listCodeReachabilityRootsSQL, want) {
			t.Fatalf("roots query missing %q:\n%s", want, listCodeReachabilityRootsSQL)
		}
	}
	for _, want := range []string{
		"entity_type = 'Class'",
		"language = 'ruby'",
		"metadata->>'qualified_name' AS qualified_name",
		"metadata->'qualified_bases' AS qualified_bases",
	} {
		if !strings.Contains(listCodeReachabilityRubyClassesSQL, want) {
			t.Fatalf("ruby classes query missing %q:\n%s", want, listCodeReachabilityRubyClassesSQL)
		}
	}
}

func TestCodeReachabilityPendingInputsWatchAllTraversedDomains(t *testing.T) {
	for _, want := range []string{
		"projection_domain IN ('code_calls', 'inheritance_edges')",
		"code_reachability_repository_watermarks",
		"watermark.updated_at",
		"max(intent.completed_at) AS completed_at",
		// #5376 P1 upgrade-backfill: the epoch aggregate + the predicate that
		// re-schedules a repo whose watermark predates the current verdict epoch.
		"max(watermark.verdict_schema_epoch) AS reach_verdict_epoch",
		"coalesce(reach_verdict_epoch, 0) < $2",
	} {
		if !strings.Contains(listPendingCodeReachabilityInputsSQL, want) {
			t.Fatalf("pending reachability query missing %q:\n%s", want, listPendingCodeReachabilityInputsSQL)
		}
	}
	// The watermark upsert must stamp the epoch column.
	for _, want := range []string{
		"verdict_schema_epoch",
		"verdict_schema_epoch = EXCLUDED.verdict_schema_epoch",
	} {
		if !strings.Contains(upsertCodeReachabilityRepositoryWatermarkSQL, want) {
			t.Fatalf("watermark upsert missing %q:\n%s", want, upsertCodeReachabilityRepositoryWatermarkSQL)
		}
	}
	if CodeReachabilityVerdictSchemaEpoch < 1 {
		t.Fatalf("CodeReachabilityVerdictSchemaEpoch = %d, want >= 1", CodeReachabilityVerdictSchemaEpoch)
	}
	if strings.Contains(upsertCodeReachabilityRepositoryWatermarkSQL, "GREATEST") {
		t.Fatalf("watermark upsert must record the committed snapshot timestamp, not hide stale rows:\n%s", upsertCodeReachabilityRepositoryWatermarkSQL)
	}
}
