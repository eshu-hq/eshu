//go:build perf5854_theory

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const containerImageIdentityCTETheoryQuery = `WITH published AS (
` + reducerFactBatchInsertQuery + `
RETURNING 1
)
DELETE FROM fact_records AS fact
WHERE fact.fact_id = ANY($17::text[])
  AND fact.fact_kind = 'reducer_container_image_identity'
  AND fact.is_tombstone = FALSE
  AND fact.scope_id = $18
  AND fact.generation_id = $19
  AND fact.fencing_token <= $20
`

const containerImageIdentityCurrentTheoryCleanupQuery = `
DELETE FROM fact_records AS fact
WHERE fact.fact_id = ANY($1::text[])
  AND fact.fact_kind = 'reducer_container_image_identity'
  AND fact.is_tombstone = FALSE
  AND fact.scope_id = $2
  AND fact.generation_id = $3
  AND fact.fencing_token <= $4
`

const (
	containerImageIdentityTheoryRows       = 500
	containerImageIdentityTheoryIterations = 20
)

func TestContainerImageIdentitySingleStatementCleanupTheoryLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #5854 CTE theory proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	currentScope := "repository:synthetic:5854:theory:current"
	currentGeneration := "generation:synthetic:5854:theory:current"
	candidateScope := "repository:synthetic:5854:theory:candidate"
	candidateGeneration := "generation:synthetic:5854:theory:candidate"
	for _, pair := range [][2]string{
		{currentScope, currentGeneration},
		{candidateScope, candidateGeneration},
	} {
		seedContainerImageIdentityTheoryParents(t, ctx, db, pair[0], pair[1])
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentityTheoryParents(
			t,
			cleanupCtx,
			db,
			[]string{currentScope, candidateScope},
		)
	})

	currentRows, currentLegacy := containerImageIdentityTheoryRowsFor(
		currentScope,
		currentGeneration,
		"current",
	)
	candidateRows, candidateLegacy := containerImageIdentityTheoryRowsFor(
		candidateScope,
		candidateGeneration,
		"candidate",
	)
	if err := execReducerFactChunk(ctx, db, currentRows); err != nil {
		t.Fatalf("seed current-path logical rows: %v", err)
	}
	if err := execReducerFactChunk(ctx, db, candidateRows); err != nil {
		t.Fatalf("seed candidate-path logical rows: %v", err)
	}

	currentDurations := make([]time.Duration, 0, containerImageIdentityTheoryIterations)
	candidateDurations := make([]time.Duration, 0, containerImageIdentityTheoryIterations)
	for range containerImageIdentityTheoryIterations {
		started := time.Now()
		deleted := executeContainerImageIdentityCurrentTheory(
			t,
			ctx,
			db,
			currentRows,
			currentLegacy,
			currentScope,
			currentGeneration,
		)
		currentDurations = append(currentDurations, time.Since(started))
		if deleted != 0 {
			t.Fatalf("steady current-path legacy deletes = %d, want 0", deleted)
		}

		started = time.Now()
		deleted = executeContainerImageIdentityCandidateTheory(
			t,
			ctx,
			db,
			candidateRows,
			candidateLegacy,
			candidateScope,
			candidateGeneration,
		)
		candidateDurations = append(candidateDurations, time.Since(started))
		if deleted != 0 {
			t.Fatalf("steady candidate-path legacy deletes = %d, want 0", deleted)
		}
	}

	seedContainerImageIdentityTheoryLegacyRows(t, ctx, db, currentRows, currentLegacy)
	seedContainerImageIdentityTheoryLegacyRows(t, ctx, db, candidateRows, candidateLegacy)
	currentDeleted := executeContainerImageIdentityCurrentTheory(
		t,
		ctx,
		db,
		currentRows,
		currentLegacy,
		currentScope,
		currentGeneration,
	)
	candidateDeleted := executeContainerImageIdentityCandidateTheory(
		t,
		ctx,
		db,
		candidateRows,
		candidateLegacy,
		candidateScope,
		candidateGeneration,
	)
	if currentDeleted != containerImageIdentityTheoryRows ||
		candidateDeleted != containerImageIdentityTheoryRows {
		t.Fatalf(
			"legacy rows deleted current/candidate = %d/%d, want %d/%d",
			currentDeleted,
			candidateDeleted,
			containerImageIdentityTheoryRows,
			containerImageIdentityTheoryRows,
		)
	}
	currentSnapshot := containerImageIdentityTheorySnapshot(
		t,
		ctx,
		db,
		currentScope,
		currentGeneration,
	)
	candidateSnapshot := containerImageIdentityTheorySnapshot(
		t,
		ctx,
		db,
		candidateScope,
		candidateGeneration,
	)
	if currentSnapshot != candidateSnapshot {
		t.Fatalf(
			"current/candidate final state differs: current=%q candidate=%q",
			currentSnapshot,
			candidateSnapshot,
		)
	}

	sort.Slice(currentDurations, func(i, j int) bool { return currentDurations[i] < currentDurations[j] })
	sort.Slice(candidateDurations, func(i, j int) bool { return candidateDurations[i] < candidateDurations[j] })
	currentMedian := currentDurations[len(currentDurations)/2]
	candidateMedian := candidateDurations[len(candidateDurations)/2]
	t.Logf(
		"CTE_THEORY rows=%d iterations=%d current_median=%s candidate_median=%s delta=%.2f%% current_statements=2+begin+commit candidate_statements=1 final_snapshot=%s",
		containerImageIdentityTheoryRows,
		containerImageIdentityTheoryIterations,
		currentMedian,
		candidateMedian,
		(float64(candidateMedian-currentMedian)/float64(currentMedian))*100,
		currentSnapshot,
	)
}

func executeContainerImageIdentityCurrentTheory(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	rows []reducerFactRow,
	legacyIDs []string,
	scopeID string,
	generationID string,
) int64 {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin current-path transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := execReducerFactChunk(ctx, tx, rows); err != nil {
		t.Fatalf("execute current-path publication: %v", err)
	}
	result, err := tx.ExecContext(
		ctx,
		containerImageIdentityCurrentTheoryCleanupQuery,
		legacyIDs,
		scopeID,
		generationID,
		rows[0].FencingToken,
	)
	if err != nil {
		t.Fatalf("execute current-path cleanup: %v", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("count current-path cleanup: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit current-path transaction: %v", err)
	}
	return deleted
}

func executeContainerImageIdentityCandidateTheory(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	rows []reducerFactRow,
	legacyIDs []string,
	scopeID string,
	generationID string,
) int64 {
	t.Helper()
	args := append(
		containerImageIdentityTheoryRowArgs(rows),
		legacyIDs,
		scopeID,
		generationID,
		rows[0].FencingToken,
	)
	result, err := db.ExecContext(ctx, containerImageIdentityCTETheoryQuery, args...)
	if err != nil {
		t.Fatalf("execute candidate-path CTE: %v", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("count candidate-path cleanup: %v", err)
	}
	return deleted
}

func containerImageIdentityTheoryRowArgs(rows []reducerFactRow) []any {
	n := len(rows)
	factIDs := make([]string, n)
	scopeIDs := make([]string, n)
	generationIDs := make([]string, n)
	factKinds := make([]string, n)
	stableKeys := make([]string, n)
	collectorKinds := make([]string, n)
	sourceConfidences := make([]string, n)
	sourceSystems := make([]string, n)
	sourceFactKeys := make([]string, n)
	sourceURIs := make([]*string, n)
	sourceRecordIDs := make([]*string, n)
	observedAts := make([]time.Time, n)
	ingestedAts := make([]time.Time, n)
	isTombstones := make([]bool, n)
	payloads := make([]string, n)
	fencingTokens := make([]int64, n)
	for i, row := range rows {
		factIDs[i] = row.FactID
		scopeIDs[i] = row.ScopeID
		generationIDs[i] = row.GenerationID
		factKinds[i] = row.FactKind
		stableKeys[i] = row.StableFactKey
		collectorKinds[i] = row.CollectorKind
		sourceConfidences[i] = row.SourceConfidence
		sourceSystems[i] = row.SourceSystem
		sourceFactKeys[i] = row.SourceFactKey
		sourceURIs[i] = row.SourceURI
		sourceRecordIDs[i] = row.SourceRecordID
		observedAts[i] = row.ObservedAt
		ingestedAts[i] = row.IngestedAt
		isTombstones[i] = row.IsTombstone
		payloads[i] = row.Payload
		fencingTokens[i] = row.FencingToken
	}
	return []any{
		factIDs, scopeIDs, generationIDs, factKinds, stableKeys,
		collectorKinds, sourceConfidences, sourceSystems, sourceFactKeys,
		sourceURIs, sourceRecordIDs, observedAts, ingestedAts,
		isTombstones, payloads, fencingTokens,
	}
}

func containerImageIdentityTheoryRowsFor(
	scopeID string,
	generationID string,
	variant string,
) ([]reducerFactRow, []string) {
	rows := make([]reducerFactRow, containerImageIdentityTheoryRows)
	legacyIDs := make([]string, containerImageIdentityTheoryRows)
	now := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	for i := range rows {
		imageRef := fmt.Sprintf("registry.example.com/performance/team-api:tag-%06d", i)
		rows[i] = reducerFactRow{
			FactID:           fmt.Sprintf("new-%s-%06d", variant, i),
			ScopeID:          scopeID,
			GenerationID:     generationID,
			FactKind:         containerImageIdentityFactKind,
			StableFactKey:    "container_image_identity:" + imageRef,
			CollectorKind:    "reducer",
			SourceConfidence: "inferred",
			SourceSystem:     "git",
			SourceFactKey:    "intent-5854-theory",
			ObservedAt:       now,
			IngestedAt:       now,
			Payload: fmt.Sprintf(
				`{"image_ref":%q,"digest":"sha256:%064x","outcome":"tag_resolved"}`,
				imageRef,
				i+1,
			),
			FencingToken: now.UnixMicro(),
		}
		legacyIDs[i] = fmt.Sprintf("legacy-%s-%06d", variant, i)
	}
	return rows, legacyIDs
}

func seedContainerImageIdentityTheoryLegacyRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	template []reducerFactRow,
	legacyIDs []string,
) {
	t.Helper()
	rows := append([]reducerFactRow(nil), template...)
	for i := range rows {
		rows[i].FactID = legacyIDs[i]
		rows[i].StableFactKey += ":tag_resolved"
	}
	if err := execReducerFactChunk(ctx, db, rows); err != nil {
		t.Fatalf("seed legacy theory rows: %v", err)
	}
}

func seedContainerImageIdentityTheoryParents(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) {
	t.Helper()
	cleanupContainerImageIdentityTheoryParents(t, ctx, db, []string{scopeID})
	now := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ($1, 'repository', 'git', $1, 'reducer', $1, $2, $2, 'active')
`, scopeID, now); err != nil {
		t.Fatalf("seed theory scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ($1, $2, 'synthetic', $3, $3, 'active')
`, generationID, scopeID, now); err != nil {
		t.Fatalf("seed theory generation: %v", err)
	}
}

func cleanupContainerImageIdentityTheoryParents(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeIDs []string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "DELETE FROM fact_records WHERE scope_id = ANY($1::text[])", scopeIDs); err != nil {
		t.Fatalf("clean theory facts: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM scope_generations WHERE scope_id = ANY($1::text[])", scopeIDs); err != nil {
		t.Fatalf("clean theory generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM ingestion_scopes WHERE scope_id = ANY($1::text[])", scopeIDs); err != nil {
		t.Fatalf("clean theory scopes: %v", err)
	}
}

func containerImageIdentityTheorySnapshot(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) string {
	t.Helper()
	var snapshot string
	if err := db.QueryRowContext(ctx, `
SELECT count(*)::text || ':' || COALESCE(
    md5(string_agg(payload->>'image_ref', E'\n' ORDER BY payload->>'image_ref')),
    md5('')
)
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = 'reducer_container_image_identity'
`, scopeID, generationID).Scan(&snapshot); err != nil {
		t.Fatalf("read theory snapshot: %v", err)
	}
	return snapshot
}
