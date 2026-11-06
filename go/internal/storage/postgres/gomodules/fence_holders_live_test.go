// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10055 real PostgreSQL proof of the fence's holder reopen: exactly the scopes holding an active finding for the repository are reopened with the origin stamp in the fence transaction, and a failed or timed-out reopen rolls the retraction back, including behind the retirement fence's table lock
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/reachability"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// seedFindingHolder writes a scope holding one active finding for the repository. A
// non-empty itemStatus adds its supply_chain_impact reducer item in that status, under
// the work item id the reducer queue mints, so the reopen's enqueue collides with it.
func seedFindingHolder(t *testing.T, database *sql.DB, holderScopeID, repositoryID, itemStatus string) string {
	t.Helper()
	ctx := t.Context()
	generationID := holderScopeID + "/g1"
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
    observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, now(), now(), 'active', '{}'::jsonb)`, holderScopeID); err != nil {
		t.Fatalf("seed holder scope: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'synthetic', now(), now(), 'active', now())`, generationID, holderScopeID); err != nil {
		t.Fatalf("seed holder generation: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, holderScopeID, generationID); err != nil {
		t.Fatalf("activate holder generation: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
    observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'reducer_supply_chain_impact_finding', $1, 'reducer', $1, now(), now(),
    jsonb_build_object('repository_id', $4::text, 'cve_id', 'CVE-2026-10055', 'package_id', 'pkg:golang/example.com/m',
        'subject_digest', '', 'runtime_reachability', 'not_called'))`,
		holderScopeID+"/finding", holderScopeID, generationID, repositoryID); err != nil {
		t.Fatalf("seed holder finding: %v", err)
	}
	if itemStatus == "" {
		return ""
	}
	clean := strings.NewReplacer(":", "_", "/", "_")
	itemID := "reducer_" + clean.Replace(holderScopeID) + "_" + clean.Replace(generationID) +
		"_supply_chain_impact_" + clean.Replace("supply_chain_impact:"+holderScopeID)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
    status, attempt_count, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'scope', $2, $4, 1, '{}'::jsonb, now(), now())`,
		itemID, holderScopeID, generationID, itemStatus); err != nil {
		t.Fatalf("seed holder item: %v", err)
	}
	return itemID
}

func readItem(t *testing.T, database *sql.DB, itemID string) (string, json.RawMessage) {
	t.Helper()
	var status string
	var origin json.RawMessage
	if err := database.QueryRowContext(t.Context(),
		`SELECT status, COALESCE(payload->'`+supplychaincore.PayloadKeyFanOutOrigin+`', 'null'::jsonb) FROM fact_work_items WHERE work_item_id = $1`,
		itemID).Scan(&status, &origin); err != nil {
		t.Fatalf("read item: %v", err)
	}
	return status, origin
}

// seedFenceable writes a repository whose evidence the rule names, and returns its fence.
func seedFenceable(t *testing.T, database *sql.DB, ordinal int) (string, scannercomponent.StaleEvidenceFence) {
	t.Helper()
	repository := seedRepository(t, database, ordinal, "go.mod")
	scopeID, generationID := seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
	seedRepositoryLeft(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval)
	return repository, scannercomponent.StaleEvidenceFence{ScopeID: scopeID, GenerationID: generationID}
}

// TestFenceReopensExactlyTheHoldersOfItsRepositoryLive is A2.4. A holder with an active
// finding for the repository is reopened with the origin stamp, and one with no item gets
// an item. A holder of another repository, a finding only on a superseded generation, and
// a dead letter are all left alone.
func TestFenceReopensExactlyTheHoldersOfItsRepositoryLive(t *testing.T) {
	database := openStoreProof(t)
	ctx := t.Context()
	repository, fence := seedFenceable(t, database, 1)

	succeeded := seedFindingHolder(t, database, "holder-succeeded", repository, "succeeded")
	seedFindingHolder(t, database, "holder-no-item", repository, "")
	deadLetter := seedFindingHolder(t, database, "holder-dead-letter", repository, "dead_letter")
	other := seedFindingHolder(t, database, "holder-other-repository", "repository:r_0000ffff", "succeeded")
	stale := seedFindingHolder(t, database, "holder-superseded-finding", repository, "succeeded")
	if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE scope_id = 'holder-superseded-finding'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = 'holder-superseded-finding'`); err != nil {
		t.Fatal(err)
	}

	result, err := fenceStore(database).FenceStaleGoEvidence(ctx, []scannercomponent.StaleEvidenceFence{fence}, time.Now().UTC())
	if err != nil || result.Fenced != 1 || result.HoldersReopened != 3 {
		t.Fatalf("result = %+v, err = %v, want 1 fenced and 3 holders (reopened, enqueued, dead letter visited)", result, err)
	}

	status, origin := readItem(t, database, succeeded)
	if status != "pending" {
		t.Fatalf("holder item status = %q, want pending", status)
	}
	var stamp map[string]string
	if err := json.Unmarshal(origin, &stamp); err != nil {
		t.Fatalf("decode origin %s: %v", origin, err)
	}
	if stamp["scope_id"] != fence.ScopeID || stamp["generation_id"] != fence.GenerationID {
		t.Fatalf("origin = %v, want the fenced scope and generation", stamp)
	}
	var enqueued int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items
WHERE scope_id = 'holder-no-item' AND generation_id = 'holder-no-item/g1' AND stage = 'reducer'
  AND domain = 'supply_chain_impact' AND status = 'pending'`).Scan(&enqueued); err != nil {
		t.Fatal(err)
	}
	if enqueued != 1 {
		t.Fatalf("items enqueued for a holder with none = %d, want 1", enqueued)
	}
	if status, _ := readItem(t, database, deadLetter); status != "dead_letter" {
		t.Fatalf("dead letter status = %q, want untouched", status)
	}
	if status, _ := readItem(t, database, other); status != "succeeded" {
		t.Fatalf("another repository's holder status = %q, want untouched", status)
	}
	if status, _ := readItem(t, database, stale); status != "succeeded" {
		t.Fatalf("a superseded finding's holder status = %q, want untouched", status)
	}
}

// TestFenceRollsBackWhenAHolderReopenFailsLive proves atomicity: a failed reopen leaves
// the generation active with its pointer, so the next pass retries the whole fence.
func TestFenceRollsBackWhenAHolderReopenFailsLive(t *testing.T) {
	database := openStoreProof(t)
	repository, fence := seedFenceable(t, database, 1)
	seedFindingHolder(t, database, "holder-a", repository, "succeeded")
	boom := errors.New("reopen failed")
	failing := NewStore(postgres.SQLDB{DB: database}).WithHolderReopener(
		func(context.Context, db.ExecQueryer, string, string, string, time.Time) error { return boom })

	result, err := failing.FenceStaleGoEvidence(t.Context(), []scannercomponent.StaleEvidenceFence{fence}, time.Now().UTC())
	if !errors.Is(err, boom) || result.Fenced != 0 {
		t.Fatalf("result = %+v, err = %v, want the reopen error and nothing fenced", result, err)
	}
	if state := readScopeState(t, database, fence.ScopeID, fence.GenerationID); state.pointer.String != fence.GenerationID || state.status != "active" {
		t.Fatalf("state = %+v, want the fence rolled back", state)
	}
}

// TestFenceTimesOutBehindAHeldHolderAndRetriesLive is the contention path. A holder's
// work row is locked by a reducer worker, so the fence's lock_timeout fires, the
// retraction rolls back, and the next pass succeeds once the row is free.
func TestFenceTimesOutBehindAHeldHolderAndRetriesLive(t *testing.T) {
	database := openStoreProof(t)
	repository, fence := seedFenceable(t, database, 1)
	itemID := seedFindingHolder(t, database, "holder-held", repository, "succeeded")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	var held string
	if err := holder.QueryRowContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`, itemID).Scan(&held); err != nil {
		t.Fatalf("hold the work row: %v", err)
	}

	fences := []scannercomponent.StaleEvidenceFence{fence}
	store := fenceStore(database)
	if result, err := store.FenceStaleGoEvidence(ctx, fences, time.Now().UTC()); err == nil || result.Fenced != 0 {
		t.Fatalf("result = %+v, err = %v, want a lock timeout and nothing fenced", result, err)
	}
	if state := readScopeState(t, database, fence.ScopeID, fence.GenerationID); state.pointer.String != fence.GenerationID || state.status != "active" {
		t.Fatalf("state after the timeout = %+v, want the fence rolled back", state)
	}

	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	result, err := store.FenceStaleGoEvidence(ctx, fences, time.Now().UTC())
	if err != nil || result.Fenced != 1 {
		t.Fatalf("retry result = %+v, err = %v, want 1 fenced", result, err)
	}
	if status, _ := readItem(t, database, itemID); status != "pending" {
		t.Fatalf("holder item status after the retry = %q, want pending", status)
	}
}

// TestFenceYieldsToTheRetirementFenceTableLockLive covers the retirement fence's order:
// scope rows, then LOCK TABLE fact_work_items. A fence on another repository waits at its
// reopen, times out, and rolls back; retirement never waits on it, so no cycle forms.
func TestFenceYieldsToTheRetirementFenceTableLockLive(t *testing.T) {
	database := openStoreProof(t)
	repository, fence := seedFenceable(t, database, 1)
	seedFindingHolder(t, database, "holder-a", repository, "succeeded")
	retired := seedRepository(t, database, 2, "go.mod")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	retirement, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = retirement.Rollback() }()
	var locked string
	if err := retirement.QueryRowContext(ctx,
		`SELECT scope_id FROM ingestion_scopes WHERE source_key = $1 FOR NO KEY UPDATE`, retired).Scan(&locked); err != nil {
		t.Fatalf("take the retiring scope: %v", err)
	}
	if _, err := retirement.ExecContext(ctx, `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`); err != nil {
		t.Fatalf("take the table lock: %v", err)
	}

	fences := []scannercomponent.StaleEvidenceFence{fence}
	store := fenceStore(database)
	if result, err := store.FenceStaleGoEvidence(ctx, fences, time.Now().UTC()); err == nil || result.Fenced != 0 {
		t.Fatalf("result = %+v, err = %v, want a lock timeout behind the table lock", result, err)
	}
	if state := readScopeState(t, database, fence.ScopeID, fence.GenerationID); state.status != "active" {
		t.Fatalf("state = %+v, want the fence rolled back", state)
	}
	if _, err := retirement.ExecContext(ctx,
		`UPDATE fact_work_items SET updated_at = now() WHERE scope_id = $1`, locked); err != nil {
		t.Fatalf("retirement proceeds after the fence yields: %v", err)
	}
	if err := retirement.Commit(); err != nil {
		t.Fatal(err)
	}
	if result, err := store.FenceStaleGoEvidence(ctx, fences, time.Now().UTC()); err != nil || result.Fenced != 1 {
		t.Fatalf("retry result = %+v, err = %v, want 1 fenced", result, err)
	}
}

// TestFiftySupersededGenerationsReadOnlyTheEvidenceCommitLive pins the probe's answer at
// the depth the EXPLAIN measures: the newest superseded_at at the evidence commit, among
// many rows at other commits.
func TestFiftySupersededGenerationsReadOnlyTheEvidenceCommitLive(t *testing.T) {
	database := openStoreProof(t)
	repository := seedRepository(t, database, 1, "go.mod")
	seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
	for i := range 50 {
		seedRepositoryLeft(t, database, repository, fmt.Sprintf("%040x", 1000+i), time.Duration(i+1)*time.Minute)
	}
	seedRepositoryLeft(t, database, repository, fenceLiveEvidenceCommit, 5*fenceLiveInterval)

	page, _ := readGoPage(t, database)
	decision := reachability.StaleEvidence(page.Repositories, time.Now().UTC(), fenceLiveInterval, page.SlotCount)
	if len(decision.Fences) != 1 || decision.HistoryPruned != 0 {
		t.Fatalf("decision = %+v, want one fence from a surviving row", decision)
	}
}
