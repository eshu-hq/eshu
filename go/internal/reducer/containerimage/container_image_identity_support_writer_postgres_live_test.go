// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestContainerImageIdentitySupportWriterLifecyclePostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the digest-v3 lifecycle proof")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	scopeID := "repository:v3-live-a"
	generationA := "generation:v3-live-a"
	generationB := "generation:v3-live-b"
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	}()
	seedContainerImageIdentityLiveScope(t, db, scopeID, generationA)
	epochA := containerImageIdentityLiveEpoch(t, db, scopeID, generationA)
	if err := insertContainerImageIdentityLegacyLiveFact(db, scopeID, generationA, digest); err != nil {
		t.Fatalf("insert pre-v3 legacy fact: %v", err)
	}
	if got := containerImageIdentityLiveVisibleCount(t, db, digest); got != 1 {
		t.Fatalf("pre-v3 legacy supports = %d, want 1", got)
	}
	seedContainerImageIdentityLiveWork(t, db, "intent:v3-live-a", scopeID, generationA, 7)

	writer := PostgresContainerImageIdentitySupportWriter{
		HeldSupportLoader: containerImageIdentityLiveHeldSupportLoader{db: db},
		ClaimedExecer:     containerImageIdentityLiveClaimedExecer{db: db},
	}
	write := containerImageIdentitySupportLiveWrite(
		"intent:v3-live-a", scopeID, generationA, 7, epochA, digest,
	)
	initialResult, err := writer.WriteContainerImageIdentityDecisions(ctx, write)
	if err != nil {
		t.Fatalf("publish support set: %v", err)
	}
	if initialResult.LegacyRowsDeleted != 1 {
		t.Fatalf("legacy rows deleted = %d, want 1", initialResult.LegacyRowsDeleted)
	}
	if err := insertContainerImageIdentityLegacyLiveFact(db, scopeID, generationA, digest); err == nil ||
		!strings.Contains(err.Error(), "digest_v3-capable scope") {
		t.Fatalf("post-v3 legacy write error = %v, want digest-v3 rejection", err)
	}

	var identityID string
	var visible int
	if err := db.QueryRowContext(ctx, `
SELECT min(identity_id), count(*)
FROM container_image_identity_current_supports
WHERE digest = $1
`, digest).Scan(&identityID, &visible); err != nil {
		t.Fatalf("read current supports: %v", err)
	}
	if visible != 3 {
		t.Fatalf("current supports = %d, want 3", visible)
	}
	wantIdentityID := reducercontract.ContainerImageIdentityFactKind + ":" + facts.StableID(
		reducercontract.ContainerImageIdentityFactKind,
		map[string]any{"digest": digest},
	)
	if identityID != wantIdentityID {
		t.Fatalf("identity_id = %q, want %q", identityID, wantIdentityID)
	}
	var foldedRepositories string
	if err := db.QueryRowContext(ctx, `
SELECT payload->'source_repository_ids'
FROM container_image_identity_current_facts_for(
    ARRAY[$1]::text[], '{}'::text[], '{}'::text[], '{}'::text[], '{}'::text[], ''::text, 500::integer
)
WHERE fact_id = $2
`, digest, identityID).Scan(&foldedRepositories); err != nil {
		t.Fatalf("read folded compatibility fact: %v", err)
	}
	if foldedRepositories != `["repository:example", "repository:second"]` {
		t.Fatalf("folded repositories = %s", foldedRepositories)
	}
	var boundedFactID string
	if err := db.QueryRowContext(ctx, `
SELECT fact_id
FROM container_image_identity_current_facts_for(
    ARRAY[$1]::text[], '{}'::text[], '{}'::text[], '{}'::text[], '{}'::text[], ''::text, 500::integer
)
`, digest).Scan(&boundedFactID); err != nil {
		t.Fatalf("read bounded compatibility fact: %v", err)
	}
	if boundedFactID != identityID {
		t.Fatalf("bounded fact id = %q, want %q", boundedFactID, identityID)
	}
	var emptyBoundCount int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM container_image_identity_current_facts_for(
    '{}'::text[], '{}'::text[], '{}'::text[], '{}'::text[], '{}'::text[], ''::text, 500::integer
)
`).Scan(&emptyBoundCount); err != nil {
		t.Fatalf("read empty bounded compatibility facts: %v", err)
	}
	if emptyBoundCount != 0 {
		t.Fatalf("empty bounded lookup returned %d rows, want 0", emptyBoundCount)
	}

	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded',
    container_image_identity_v2_authorized_status = 'succeeded',
    container_image_identity_v3_authorized_status = 'succeeded'
WHERE work_item_id = 'intent:v3-live-a'
`); err != nil {
		t.Fatalf("finish initial work item: %v", err)
	}
	seedContainerImageIdentityLiveWork(t, db, "intent:v3-live-held", scopeID, generationA, 8)
	heldDecision := ContainerImageIdentityDecision{
		ImageRef: write.Decisions[1].ImageRef,
		Outcome:  reducercontract.ContainerImageIdentityUnresolved,
	}
	heldWrite := containerImageIdentitySupportLiveWrite(
		"intent:v3-live-held", scopeID, generationA, 8, epochA, digest,
	)
	heldWrite.Decisions = []ContainerImageIdentityDecision{
		write.Decisions[0],
		heldDecision,
		write.Decisions[2],
	}
	heldWrite.HeldDecisions = []ContainerImageIdentityDecision{heldDecision}
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, heldWrite); err != nil {
		t.Fatalf("publish support set with held prior reference: %v", err)
	}
	if got := containerImageIdentityLiveVisibleCount(t, db, digest); got != 3 {
		t.Fatalf("held publication exposed %d supports, want prior held support plus 2 current supports", got)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded',
    container_image_identity_v2_authorized_status = 'succeeded',
    container_image_identity_v3_authorized_status = 'succeeded'
WHERE work_item_id = 'intent:v3-live-held'
`); err != nil {
		t.Fatalf("finish held work item: %v", err)
	}
	seedContainerImageIdentityLiveWork(t, db, "intent:v3-live-held-clear", scopeID, generationA, 9)
	clearedWrite := containerImageIdentitySupportLiveWrite(
		"intent:v3-live-held-clear", scopeID, generationA, 9, epochA, digest,
	)
	clearedWrite.Decisions = []ContainerImageIdentityDecision{
		write.Decisions[0],
		write.Decisions[2],
	}
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, clearedWrite); err != nil {
		t.Fatalf("publish support set after warning clears: %v", err)
	}
	if got := containerImageIdentityLiveVisibleCount(t, db, digest); got != 2 {
		t.Fatalf("cleared publication exposed %d supports, want held support retired", got)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded',
    container_image_identity_v2_authorized_status = 'succeeded',
    container_image_identity_v3_authorized_status = 'succeeded'
WHERE work_item_id = 'intent:v3-live-held-clear'
`); err != nil {
		t.Fatalf("finish warning-clear work item: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'failed' WHERE generation_id = $1
`, generationA); err != nil {
		t.Fatalf("mark generation non-active: %v", err)
	}
	if got := containerImageIdentityLiveVisibleCount(t, db, digest); got != 0 {
		t.Fatalf("non-active generation exposed %d supports, want 0", got)
	}
	seedContainerImageIdentityLiveWork(t, db, "intent:v3-live-non-active", scopeID, generationA, 10)
	nonActiveWrite := containerImageIdentitySupportLiveWrite(
		"intent:v3-live-non-active", scopeID, generationA, 10, epochA, digest,
	)
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, nonActiveWrite); err != ErrContainerImageIdentityClaimRejected {
		t.Fatalf("non-active generation publication error = %v, want claim rejected", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'failed',
    container_image_identity_v2_authorized_status = 'failed',
    container_image_identity_v3_authorized_status = 'failed'
WHERE work_item_id = 'intent:v3-live-non-active'
`); err != nil {
		t.Fatalf("finish non-active work item: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'active' WHERE generation_id = $1
`, generationA); err != nil {
		t.Fatalf("restore active generation: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded',
    container_image_identity_v2_authorized_status = 'succeeded',
    container_image_identity_v3_authorized_status = 'succeeded'
WHERE work_item_id = 'intent:v3-live-a'
`); err != nil {
		t.Fatalf("finish first work item: %v", err)
	}
	seedContainerImageIdentityLiveGeneration(t, db, scopeID, generationB, "pending")
	abaStatements := []struct {
		query string
		args  []any
	}{
		{`UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1`, []any{generationA}},
		{`UPDATE scope_generations SET status = 'active' WHERE generation_id = $1`, []any{generationB}},
		{`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, []any{generationB, scopeID}},
		{`UPDATE scope_generations SET status = 'pending' WHERE generation_id = $1`, []any{generationB}},
		{`UPDATE scope_generations SET status = 'active' WHERE generation_id = $1`, []any{generationA}},
		{`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, []any{generationA, scopeID}},
	}
	seedContainerImageIdentityLiveWork(t, db, "intent:v3-live-stale", scopeID, generationA, 11)
	staleWrite := containerImageIdentitySupportLiveWrite(
		"intent:v3-live-stale", scopeID, generationA, 11, epochA, digest,
	)
	staleWrite.HeldDecisions = []ContainerImageIdentityDecision{{
		ImageRef: write.Decisions[0].ImageRef,
		Outcome:  reducercontract.ContainerImageIdentityUnresolved,
	}}
	staleLoader := &containerImageIdentityActivatingHeldSupportLoader{
		base: containerImageIdentityLiveHeldSupportLoader{db: db},
		afterLoad: func() error {
			for _, statement := range abaStatements {
				if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
					return err
				}
			}
			return nil
		},
	}
	staleWriter := PostgresContainerImageIdentitySupportWriter{
		HeldSupportLoader: staleLoader,
		ClaimedExecer:     containerImageIdentityLiveClaimedExecer{db: db},
	}
	if _, err := staleWriter.WriteContainerImageIdentityDecisions(ctx, staleWrite); err != ErrContainerImageIdentityClaimRejected {
		t.Fatalf("stale activation error = %v, want claim rejected", err)
	}
	if staleLoader.loaded != 1 {
		t.Fatalf("supports loaded before activation change = %d, want 1", staleLoader.loaded)
	}
	epochA2 := containerImageIdentityLiveEpoch(t, db, scopeID, generationA)
	if epochA2 <= epochA {
		t.Fatalf("ABA epoch = %d, want greater than %d", epochA2, epochA)
	}
	if got := containerImageIdentityLiveVisibleCount(t, db, digest); got != 0 {
		t.Fatalf("activation did not immediately invalidate prior set: %d rows", got)
	}

	freshWrite := staleWrite
	freshWrite.ActivationEpoch = epochA2
	freshWrite.Decisions = nil
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, freshWrite); err != nil {
		t.Fatalf("publish zero-output set: %v", err)
	}
	var activeSet bool
	if err := db.QueryRowContext(ctx, `
SELECT active_set_id IS NOT NULL
FROM container_image_identity_scope_state
WHERE scope_id = $1
`, scopeID).Scan(&activeSet); err != nil {
		t.Fatalf("read zero-output state: %v", err)
	}
	if !activeSet {
		t.Fatal("zero-output publication did not install an explicit empty set")
	}
	var shadowRows int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_records WHERE fact_kind = 'reducer_container_image_identity'
  AND scope_id = $1
`, scopeID).Scan(&shadowRows); err != nil {
		t.Fatalf("count fact shadow rows: %v", err)
	}
	if shadowRows != 0 {
		t.Fatalf("digest-v3 wrote %d fact_records shadow rows", shadowRows)
	}
}

func TestContainerImageIdentitySupportWriterCarriesHeldLegacySupportPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the digest-v3 legacy hold proof")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	scopeID := "repository:v3-live-legacy-hold"
	generationID := "generation:v3-live-legacy-hold"
	digest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	}()
	seedContainerImageIdentityLiveScope(t, db, scopeID, generationID)
	epoch := containerImageIdentityLiveEpoch(t, db, scopeID, generationID)
	if err := insertContainerImageIdentityLegacyLiveFact(db, scopeID, generationID, digest); err != nil {
		t.Fatalf("insert held legacy fact: %v", err)
	}
	seedContainerImageIdentityLiveWork(t, db, "intent:v3-live-legacy-hold", scopeID, generationID, 11)

	writer := PostgresContainerImageIdentitySupportWriter{
		HeldSupportLoader: containerImageIdentityLiveHeldSupportLoader{db: db},
		ClaimedExecer:     containerImageIdentityLiveClaimedExecer{db: db},
	}
	result, err := writer.WriteContainerImageIdentityDecisions(ctx, ContainerImageIdentityWrite{
		IntentID:        "intent:v3-live-legacy-hold",
		ClaimEpoch:      11,
		ActivationEpoch: epoch,
		ScopeID:         scopeID,
		GenerationID:    generationID,
		SourceSystem:    "git",
		Cause:           "live_test",
		EvidenceAsOf:    time.Unix(1_700_000_000, 0).UTC(),
		HeldDecisions: []ContainerImageIdentityDecision{{
			ImageRef: "registry.example.com/team/legacy@" + digest,
			Outcome:  reducercontract.ContainerImageIdentityUnresolved,
		}},
	})
	if err != nil {
		t.Fatalf("publish held legacy support: %v", err)
	}
	if result.CanonicalWrites != 0 || result.LegacyRowsDeleted != 1 {
		t.Fatalf("write result = %#v, want retained-only publication and one cleanup", result)
	}
	if got := containerImageIdentityLiveVisibleCount(t, db, digest); got != 1 {
		t.Fatalf("held legacy publication exposed %d supports, want 1", got)
	}
	var legacyRows int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_records
WHERE scope_id = $1 AND fact_kind = 'reducer_container_image_identity'
`, scopeID).Scan(&legacyRows); err != nil {
		t.Fatalf("count cleaned legacy rows: %v", err)
	}
	if legacyRows != 0 {
		t.Fatalf("legacy rows after authority switch = %d, want 0", legacyRows)
	}
}
