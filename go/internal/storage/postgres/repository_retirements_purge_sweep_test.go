// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// purgeExecOrder runs the scripted purge and returns the executed statements
// in the order the transaction received them (execs only: every leftover
// delete, the advisory locks and the scope delete are execs, so the index is
// chronological).
func purgeExecOrder(t *testing.T) ([]string, []fakeExecCall) {
	t.Helper()
	database, tx := stagePurgeScriptedDB()
	purger := NewRetirementPurger(database, database)
	if _, err := purger.PurgeRetired(context.Background(), purgeTestRequest()); err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	order := make([]string, 0, len(tx.execs))
	for _, call := range tx.execs {
		// The advisory-lock key is an argument, so the argument text joins the
		// statement the assertions search.
		order = append(order, call.query+" "+fmt.Sprint(call.args...))
	}
	return order, tx.execs
}

func purgeIndexOf(order []string, fragment string) int {
	for i, stmt := range order {
		if strings.Contains(stmt, fragment) {
			return i
		}
	}
	return -1
}

// TestPurgeRetiredOrdersTheCensus2eStatements pins the position of each #10820
// statement whose order is load-bearing. Each assertion names the reason, and
// the live tests prove the consequence against real PostgreSQL.
func TestPurgeRetiredOrdersTheCensus2eStatements(t *testing.T) {
	t.Parallel()

	order, _ := purgeExecOrder(t)
	at := func(fragment string) int {
		t.Helper()
		idx := purgeIndexOf(order, fragment)
		if idx < 0 {
			t.Fatalf("purge never issued %q:\n%s", fragment, strings.Join(order, "\n"))
		}
		return idx
	}
	contentFiles := at("DELETE FROM content_files")
	contentEntities := at("DELETE FROM content_entities")
	scopeDelete := at("DELETE FROM ingestion_scopes")
	completion := at("SET state = 'complete'")

	// The derive advisory locks come before any content row is touched, in
	// retention's order: lock, then content rows.
	infraLock := at("infra_resource_entities:")
	configLock := at("config_artifact_paths:")
	if infraLock >= contentFiles || configLock >= contentFiles {
		t.Errorf("advisory locks (infra %d, config %d) must precede the first content delete (%d)", infraLock, configLock, contentFiles)
	}
	// The content_entities delete re-marks the repository dirty through the
	// migration 109 row trigger, so the mark is cleared after it.
	if dirtyRepos := at("DELETE FROM infra_resource_entity_dirty_repos"); dirtyRepos <= contentEntities {
		t.Errorf("infra dirty-mark delete (%d) must follow the content_entities delete (%d)", dirtyRepos, contentEntities)
	}
	// The consumption dirty-scope mark is cleared after the cascade whose
	// fact_records trigger can write it (defensive: the sidecar prune's session
	// marker suppresses it today, so no live mutant separates the two orders).
	// relationship_generations goes after the cascade because the writer locks
	// its work row first and the cascade has locked the work rows by then; a
	// live probe turns red when it moves.
	for _, fragment := range []string{
		"DELETE FROM package_manifest_consumption_key_dirty_scopes",
		"DELETE FROM relationship_generations",
	} {
		if idx := at(fragment); idx <= scopeDelete || idx >= completion {
			t.Errorf("%s (idx %d) must follow the scope delete (%d) and precede the completion (%d)", fragment, idx, scopeDelete, completion)
		}
	}
	// The relationship tables read relationship_generations for their
	// generation ids, so they delete first; they delete by generation id
	// while scope_generations still exists.
	generations := at("DELETE FROM relationship_generations")
	for _, fragment := range []string{
		"DELETE FROM relationship_evidence_facts",
		"DELETE FROM relationship_candidates",
	} {
		if idx := at(fragment); idx >= scopeDelete || idx >= generations {
			t.Errorf("%s (idx %d) must precede the scope delete (%d) and the generation map delete (%d)", fragment, idx, scopeDelete, generations)
		}
	}
	// Evidence before its parent decision rows.
	if at("DELETE FROM projection_decision_evidence") >= at("DELETE FROM projection_decisions") {
		t.Error("projection_decision_evidence must delete before its projection_decisions parent")
	}
	// Every other #10820 leftover sits in the sweep before the cascade.
	for _, fragment := range []string{
		"DELETE FROM reducer_readiness_waits",
		"DELETE FROM reducer_waiting_items",
		"DELETE FROM crossplane_satisfied_by_redrive_state",
		"DELETE FROM crossplane_satisfied_by_redrive_target_ledger",
		"DELETE FROM service_materialization_generations",
		"DELETE FROM code_function_fingerprint",
		"DELETE FROM code_fingerprint_band",
		"DELETE FROM content_file_ref_deltas",
		"DELETE FROM content_file_ref_delta_bases",
		"DELETE FROM function_summaries",
		"DELETE FROM function_sources",
		"DELETE FROM function_graph_ids",
		"DELETE FROM infra_resource_entities",
		"DELETE FROM config_artifact_paths",
	} {
		if idx := at(fragment); idx >= scopeDelete {
			t.Errorf("%s (idx %d) must precede the scope delete (%d)", fragment, idx, scopeDelete)
		}
	}
}

// TestPurgeRetiredLedgerReportsTheCensus2eTables pins that the tombstone
// ledger carries a count for every #10820 table (the scripted rows affected is
// zero, so the key must still be present) and the snapshot pre-count.
func TestPurgeRetiredLedgerReportsTheCensus2eTables(t *testing.T) {
	t.Parallel()

	_, execs := purgeExecOrder(t)
	var ledgerJSON string
	for _, call := range execs {
		if strings.Contains(call.query, "SET state = 'complete'") {
			ledgerJSON, _ = call.args[1].(string)
		}
	}
	var ledger map[string]int64
	if err := json.Unmarshal([]byte(ledgerJSON), &ledger); err != nil {
		t.Fatalf("decode ledger %q: %v", ledgerJSON, err)
	}
	for _, table := range []string{
		"reducer_readiness_waits", "reducer_waiting_items", "projection_decisions", "projection_decision_evidence",
		"crossplane_satisfied_by_redrive_state", "crossplane_satisfied_by_redrive_target_ledger",
		"relationship_generations", "relationship_evidence_facts", "relationship_candidates",
		"service_materialization_generations", "service_evidence_snapshots",
		"config_artifact_paths", "content_file_ref_deltas", "content_file_ref_delta_bases",
		"code_function_fingerprint", "code_fingerprint_band", "function_summaries", "function_sources",
		"function_graph_ids", "infra_resource_entities", "infra_resource_entity_dirty_repos",
		"package_manifest_consumption_key_dirty_scopes",
	} {
		if _, ok := ledger[table]; !ok {
			t.Errorf("ledger has no %s count (full ledger %v)", table, ledger)
		}
	}
	if ledger["service_evidence_snapshots"] != 3 {
		t.Errorf("service_evidence_snapshots = %d, want the staged pre-count 3", ledger["service_evidence_snapshots"])
	}
}

// TestPurgeRetiredLogsTheDeriveLockWait pins the operator signal for the
// advisory-lock wait the purge added (#10820): the lock wait is unbounded and
// holds the scope lock, so the completion log carries its duration.
func TestPurgeRetiredLogsTheDeriveLockWait(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	database, _ := stagePurgeScriptedDB()
	purger := NewRetirementPurger(database, database)
	purger.Options.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	if _, err := purger.PurgeRetired(context.Background(), purgeTestRequest()); err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	for _, field := range []string{"derive_lock_wait_ms=", "purge_duration_ms="} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("completion log has no %s field:\n%s", field, logs.String())
		}
	}
}
