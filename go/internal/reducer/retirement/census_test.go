// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"strings"
	"testing"
)

// TestCensusClassifiesVulnerabilityTargetRefreshProgress pins the #9879
// progress table's row: its scope ids are configured
// vulnerability-intelligence targets, so repository retirement keeps it
// like its vulnerability_source_states sibling.
func TestCensusClassifiesVulnerabilityTargetRefreshProgress(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("vulnerability_target_refresh_progress")
	if !ok {
		t.Fatal("vulnerability_target_refresh_progress has no census row; the live census fails on it")
	}
	if entry.Disposition != "kept" || len(entry.RemoveSteps) != 0 {
		t.Fatalf("disposition = %q steps %v, want kept with no remove steps", entry.Disposition, entry.RemoveSteps)
	}
	if len(entry.KeyColumns) != 1 || entry.KeyColumns[0] != "scope_id" {
		t.Fatalf("key columns = %v, want [scope_id]", entry.KeyColumns)
	}
}

// TestCensusClassifiesNVDBaselineCheckpoints pins the #10809 checkpoint row:
// migration 301 adds last_scope_id, which matches the live census's %scope%
// column filter and pulls the table into the census, but it names a
// vulnerability-intelligence baseline chunk (an audit pointer), not a
// repository scope. The table is not_a_key with no purge step.
func TestCensusClassifiesNVDBaselineCheckpoints(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("vulnerability_nvd_baseline_checkpoints")
	if !ok {
		t.Fatal("vulnerability_nvd_baseline_checkpoints has no census row; the live census fails on its last_scope_id column")
	}
	if entry.Disposition != "not_a_key" || len(entry.RemoveSteps) != 0 {
		t.Fatalf("disposition = %q steps %v, want not_a_key with no remove steps", entry.Disposition, entry.RemoveSteps)
	}
	if len(entry.KeyColumns) != 1 || entry.KeyColumns[0] != "last_scope_id" {
		t.Fatalf("key columns = %v, want [last_scope_id], the column the live filter matched", entry.KeyColumns)
	}
	if !strings.Contains(entry.Reason, "audit pointer") {
		t.Fatalf("reason %q must say last_scope_id is an audit pointer, not a repository scope", entry.Reason)
	}
}

// TestCensusClassifiesWinnersDirtyTally pins the #10767 drain tally's row:
// nvd_scope_rows matches the live census's %scope% column filter but is a
// count of drained NVD pointer-flip rows, not a scope or repository
// identity, so the table is not_a_key (design 7766) with no purge step. The
// maintainer prunes its own buckets after three hours.
func TestCensusClassifiesWinnersDirtyTally(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("supply_chain_impact_winners_dirty_tally")
	if !ok {
		t.Fatal("supply_chain_impact_winners_dirty_tally has no census row; the live census fails on its nvd_scope_rows column")
	}
	if entry.Disposition != "not_a_key" || len(entry.RemoveSteps) != 0 {
		t.Fatalf("disposition = %q steps %v, want not_a_key with no remove steps", entry.Disposition, entry.RemoveSteps)
	}
	if len(entry.KeyColumns) != 1 || entry.KeyColumns[0] != "nvd_scope_rows" {
		t.Fatalf("key columns = %v, want [nvd_scope_rows], the column the live filter matched", entry.KeyColumns)
	}
	if !strings.Contains(entry.Reason, "count") {
		t.Fatalf("reason %q must say nvd_scope_rows is a count, not a scope key", entry.Reason)
	}
}

// TestCensusClassifiesSupplyChainImpactScopePassState pins the #10389 pass
// state table's row (#10794): it is keyed by scope_id with no foreign key,
// and a surviving partial row would hold the stuck-partial degraded reason
// for a purged scope forever, so the purge deletes it at 2e.
func TestCensusClassifiesSupplyChainImpactScopePassState(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("supply_chain_impact_scope_pass_state")
	if !ok {
		t.Fatal("supply_chain_impact_scope_pass_state has no census row; the live census fails on it")
	}
	if entry.Disposition != "2e" {
		t.Fatalf("disposition = %q, want 2e", entry.Disposition)
	}
	if len(entry.RemoveSteps) != 1 || entry.RemoveSteps[0] != "2e" {
		t.Fatalf("remove steps = %v, want [2e]", entry.RemoveSteps)
	}
	if len(entry.KeyColumns) != 1 || entry.KeyColumns[0] != "scope_id" {
		t.Fatalf("key columns = %v, want [scope_id]", entry.KeyColumns)
	}
	if entry.InvisibleToScan {
		t.Fatal("InvisibleToScan = true, want false: the schema scan sees scope_id")
	}
}

// TestCensusClassifiesSupplyChainImpactWriteAdmission pins the #7142 admission
// watermark table's row (#10800): scope-keyed with no foreign key, deleted by
// the purge at 2e. Unlike the pass-state row, the delete must run BEFORE the
// ingestion_scopes delete, because the impact writer upserts this row before
// its fact insert takes the scope_generations KEY SHARE. The reason text names
// that order so the census cannot drift back to a sweep nobody runs.
func TestCensusClassifiesSupplyChainImpactWriteAdmission(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("supply_chain_impact_write_admission")
	if !ok {
		t.Fatal("supply_chain_impact_write_admission has no census row")
	}
	if entry.Disposition != "2e" {
		t.Fatalf("disposition = %q, want 2e", entry.Disposition)
	}
	if len(entry.RemoveSteps) != 1 || entry.RemoveSteps[0] != "2e" {
		t.Fatalf("remove steps = %v, want [2e]", entry.RemoveSteps)
	}
	if len(entry.KeyColumns) != 1 || entry.KeyColumns[0] != "scope_id" {
		t.Fatalf("key columns = %v, want [scope_id]", entry.KeyColumns)
	}
	if entry.InvisibleToScan {
		t.Fatal("InvisibleToScan = true, want false: the schema scan sees scope_id")
	}
	if !strings.Contains(entry.Reason, "before the ingestion_scopes delete") {
		t.Fatalf("reason = %q, want it to name the delete order (before the ingestion_scopes delete)", entry.Reason)
	}
}

// TestRetireTableDispositionsAreWellFormed pins the census table's shape:
// every row names its table, carries a valid disposition code with only
// known purge steps, and cites its deleter or keep reason. The live test
// in the Postgres package proves coverage against the migrated schema;
// this one proves the rows themselves are coherent.
func TestRetireTableDispositionsAreWellFormed(t *testing.T) {
	t.Parallel()

	if len(RetireTableDispositions) == 0 {
		t.Fatal("census table is empty; the live comparison below would pass having checked nothing")
	}
	validDispositions := map[string]struct{}{
		"2b": {}, "2d": {}, "2e": {}, "3": {}, "3+count": {},
		"reaped": {}, "kept": {}, "not_a_key": {}, "deferred": {},
	}
	validSteps := map[string]struct{}{"2b": {}, "2d": {}, "2e": {}, "3": {}}
	for table, entry := range RetireTableDispositions {
		if entry.Table != table {
			t.Errorf("census row key %q carries table %q", table, entry.Table)
		}
		if _, ok := validDispositions[entry.Disposition]; !ok {
			t.Errorf("table %q has unknown disposition %q", table, entry.Disposition)
		}
		for _, step := range entry.RemoveSteps {
			if _, ok := validSteps[step]; !ok {
				t.Errorf("table %q lists unknown remove step %q", table, step)
			}
		}
		if strings.TrimSpace(entry.Reason) == "" {
			t.Errorf("table %q has no reason", table)
		}
		switch entry.Disposition {
		case "kept", "reaped", "not_a_key", "deferred":
			if len(entry.RemoveSteps) != 0 {
				t.Errorf("table %q is %q but lists remove steps %v", table, entry.Disposition, entry.RemoveSteps)
			}
		default:
			if len(entry.RemoveSteps) == 0 {
				t.Errorf("table %q is %q but lists no remove step", table, entry.Disposition)
			}
		}
		if _, ok := RetireTableDispositionFor(table); !ok {
			t.Errorf("RetireTableDispositionFor(%q) misses a table the map holds", table)
		}
	}
}

// TestCensusRowsRecordTheirPurgeOrder pins the #10820 rows whose delete position
// is load-bearing: each reason must name the order, so a later edit that moves
// the statement has to rewrite the claim as well. The positions come from the
// writers' lock order and from the triggers that re-create the rows
// (docs/internal/evidence/10820-census-2e-purge.md).
func TestCensusRowsRecordTheirPurgeOrder(t *testing.T) {
	t.Parallel()

	for table, phrases := range map[string][]string{
		"relationship_generations":                      {"after the ingestion_scopes delete", "relationship_evidence_facts"},
		"package_manifest_consumption_key_dirty_scopes": {"after the ingestion_scopes delete", "fact_records"},
		"infra_resource_entity_dirty_repos":             {"after the content_entities delete", "content_entities_infra_dirty_delete"},
		"infra_resource_entities":                       {"before the content_entities delete", "advisory lock"},
		"config_artifact_paths":                         {"before the content_files delete", "advisory lock"},
		"service_materialization_generations":           {"before the ingestion_scopes delete", "service_evidence_snapshots"},
	} {
		entry, ok := RetireTableDispositionFor(table)
		if !ok {
			t.Fatalf("%s has no census row", table)
		}
		if entry.Disposition != "2e" || len(entry.RemoveSteps) != 1 || entry.RemoveSteps[0] != "2e" {
			t.Fatalf("%s: disposition %q steps %v, want 2e with the single 2e step", table, entry.Disposition, entry.RemoveSteps)
		}
		for _, phrase := range phrases {
			if !strings.Contains(entry.Reason, phrase) {
				t.Errorf("%s: reason %q does not name %q", table, entry.Reason, phrase)
			}
		}
	}
}

// TestCensusDeferredRowsNameTheirBlockingQuestion pins the rows #10820 does not
// purge because the work they wait on is tracked elsewhere. A deferred row keeps
// no remove step, so the census-versus-purge guard does not demand a statement
// for it, but it must say what blocks the delete, cite the evidence note, and
// cite the open issue that tracks the blocker, so the row cannot quietly become
// a permanent exemption. The list is closed: a new deferred row needs an edit
// here.
func TestCensusDeferredRowsNameTheirBlockingQuestion(t *testing.T) {
	t.Parallel()

	type blocker struct{ phrase, issue string }
	want := map[string]blocker{
		"resolved_relationships":             {"append-only", "#10823"},
		"projected_source_edge":              {"graph retraction", "#10822"},
		"code_interproc_projected_edge":      {"graph retraction", "#10822"},
		"code_taint_evidence_projected_node": {"graph retraction", "#10822"},
	}
	deferred := make(map[string]struct{})
	for table, entry := range RetireTableDispositions {
		if entry.Disposition == "deferred" {
			deferred[table] = struct{}{}
		}
	}
	for table, b := range want {
		entry, ok := RetireTableDispositionFor(table)
		if !ok {
			t.Fatalf("%s has no census row", table)
		}
		if entry.Disposition != "deferred" || len(entry.RemoveSteps) != 0 {
			t.Errorf("%s: disposition %q steps %v, want deferred with no steps", table, entry.Disposition, entry.RemoveSteps)
		}
		if !strings.Contains(entry.Reason, b.phrase) {
			t.Errorf("%s: reason %q does not name the blocker %q", table, entry.Reason, b.phrase)
		}
		if !strings.Contains(entry.Reason, b.issue) {
			t.Errorf("%s: reason %q does not cite the tracking issue %s", table, entry.Reason, b.issue)
		}
		if !strings.Contains(entry.Reason, "10820-census-2e-purge.md") {
			t.Errorf("%s: reason %q does not cite the evidence note", table, entry.Reason)
		}
		delete(deferred, table)
	}
	if len(deferred) != 0 {
		t.Errorf("deferred rows with no recorded blocker in this test: %v", deferred)
	}
}

// TestCensusConfigArtifactFenceRowsAreKept pins the #10820 ruling that the
// config-artifact version and state rows stay: the purge deletes neither, the
// commit trigger bumps the version so a derive that read before the purge is
// stale, and the walker converges the state to an empty derive. Deleting the
// version row would orphan a late derive write and restart the counter at 1.
func TestCensusConfigArtifactFenceRowsAreKept(t *testing.T) {
	t.Parallel()

	for _, table := range []string{"config_artifact_content_versions", "config_artifact_repo_state"} {
		entry, ok := RetireTableDispositionFor(table)
		if !ok {
			t.Fatalf("%s has no census row", table)
		}
		if entry.Disposition != "kept" || len(entry.RemoveSteps) != 0 {
			t.Errorf("%s: disposition %q steps %v, want kept with no steps", table, entry.Disposition, entry.RemoveSteps)
		}
		for _, phrase := range []string{"monotonic", "walker", "config_artifact_fence_pending_commit"} {
			if !strings.Contains(entry.Reason, phrase) {
				t.Errorf("%s: reason %q does not name %q", table, entry.Reason, phrase)
			}
		}
	}
	paths, _ := RetireTableDispositionFor("config_artifact_paths")
	if strings.Contains(paths.Reason, "rejects a derive that read before the purge") {
		t.Errorf("config_artifact_paths reason claims the fence rejects an earlier derive: %q", paths.Reason)
	}
	for _, phrase := range []string{"late write", "walker"} {
		if !strings.Contains(paths.Reason, phrase) {
			t.Errorf("config_artifact_paths reason %q does not state %q", paths.Reason, phrase)
		}
	}
}

// TestCensusResolvedRelationshipsReasonIsAccurate pins the reader statement for
// the deferred resolved_relationships row: generation-joined readers and the
// fingerprint lose the rows once relationship_generations is deleted, but
// repositoryDependencyCount reads by source_repo_id with no generation join, so
// the rows are not "unread".
func TestCensusResolvedRelationshipsReasonIsAccurate(t *testing.T) {
	t.Parallel()

	entry, _ := RetireTableDispositionFor("resolved_relationships")
	if strings.Contains(entry.Reason, "unread") {
		t.Errorf("reason still says the rows are unread: %q", entry.Reason)
	}
	for _, phrase := range []string{"repositoryDependencyCount", "#10824"} {
		if !strings.Contains(entry.Reason, phrase) {
			t.Errorf("reason %q does not name %q", entry.Reason, phrase)
		}
	}
}

// TestCensusWinnersAreReapedByTheKeyedResweep pins the #10820 correction of the
// canonical winners row: nothing purges it, because the BEFORE DELETE trigger on
// ingestion_scopes (migration 272) logs the scope's identifiers and the keyed
// resweep removes the winners they named.
func TestCensusWinnersAreReapedByTheKeyedResweep(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("supply_chain_impact_canonical_winners")
	if !ok {
		t.Fatal("supply_chain_impact_canonical_winners has no census row")
	}
	if entry.Disposition != "reaped" || len(entry.RemoveSteps) != 0 {
		t.Fatalf("disposition %q steps %v, want reaped with no steps", entry.Disposition, entry.RemoveSteps)
	}
	for _, phrase := range []string{"ingestion_scopes_winners_dirty_capture_delete", "TestSupplyChainImpactWinnersResweepRetirementFenceThenPurgeLive"} {
		if !strings.Contains(entry.Reason, phrase) {
			t.Errorf("reason %q does not cite %q", entry.Reason, phrase)
		}
	}
}
