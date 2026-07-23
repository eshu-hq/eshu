// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// ociTagFirstObservedProveEnv gates the #5459 prove-theory-first shim. The test
// is skipped by default; set it to run against a live NornicDB.
const ociTagFirstObservedProveEnv = "ESHU_OCI_TAG_PROVE_LIVE"

// ociTagFirstObservedOnCreateUpsert is the exact set-once shape the #5459
// tag-observation writer adopts for first_observed_at, the first queryable
// node-property timestamp in the canonical graph. It mirrors the production
// canonicalOCIImageTagObservationUpsertCypher's first_observed_at handling.
//
// The ContainerImageTagObservation node dedups every observation of a
// (repository_id, tag, resolved_digest) into ONE node keyed by a stable uid, so
// the timestamp on that node must be fixed at first projection and never
// overwritten. Three NornicDB realities were disproven live before this shape
// settled:
//
//  1. A compound self-referencing guard
//     (SET t.x = CASE WHEN t.x IS NULL OR row.v < t.x THEN row.v ELSE t.x END)
//     inside UNWIND...MERGE...SET did NOT evaluate the self-read on the pinned
//     NornicDB binary — it stored the literal token and regressed to
//     last-write-wins.
//  2. coalesce(t.x, row.v) inside the SAME UNWIND...MERGE...SET also regressed:
//     the MERGE binding shadows the persisted property as null within one
//     statement, so every self-read sees null.
//  3. A separate DEFERRED second-transaction MATCH...WHERE t.first_observed_at
//     IS NULL...SET (which passed this shim in isolation) still failed the live
//     golden-corpus pipeline: the deferred MATCH did not surface the
//     multi-label node the identity MERGE created across the write group's
//     transaction boundary, so first_observed_at was never populated.
//
// ON CREATE SET fixes all three: it reads NO persisted property (it fires only
// when the node is created), so there is no self-reference to shadow and no
// cross-transaction MATCH to miss. It is genuine set-once in the single identity
// MERGE, idempotent under replay, and concurrency-robust (a re-MERGE of an
// existing node never re-fires ON CREATE). Under the reducer's in-order
// generation processing the first projected observation is the chronologically
// earliest, so first-created == first-observed. A back-dated observation
// arriving after a later one is a documented limitation, tracked as a follow-up
// alongside last_observed_at and true per-event history.
const ociTagFirstObservedOnCreateUpsert = `UNWIND $rows AS row
MERGE (t:ContainerImageTagObservation:OciImageTagObservation {uid: row.uid})
ON CREATE SET t.first_observed_at = row.observed_at
SET t.tag = row.tag,
    t.resolved_digest = row.resolved_digest,
    t.image_ref = row.image_ref`

// TestLiveOCITagFirstObservedProveTheory proves, against a live NornicDB, that
// the #5459 ON CREATE SET holds first_observed_at at the FIRST projected
// observation and never overwrites it under later or out-of-order
// re-projection. It upserts the same uid three times — t2 (first), then an
// earlier t1, then a later t3 — and asserts first_observed_at stayed at t2 (the
// value present when the node was created). A bare SET would leave it at t3; a
// self-read inside UNWIND+MERGE leaves last-write or a literal; a deferred MATCH
// silently leaves it null in the real pipeline. Only ON CREATE SET holds the
// first value in a single statement with no cross-transaction dependency.
//
// Skipped unless ESHU_OCI_TAG_PROVE_LIVE=1 with the Common Compose Environment
// (NEO4J_URI/NEO4J_USERNAME/NEO4J_PASSWORD/ESHU_NEO4J_DATABASE) at a live
// NornicDB.
func TestLiveOCITagFirstObservedProveTheory(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ociTagFirstObservedProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5459 tag-observation ON CREATE SET prove-theory shim", ociTagFirstObservedProveEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = driver.Close(closeCtx)
	}()

	const uid = "oci-tag-observation-prove-5459"
	const (
		t1 = "2026-06-20T00:00:00Z" // earliest value, but written SECOND
		t2 = "2026-06-25T00:00:00Z" // written FIRST -> must win
		t3 = "2026-06-30T00:00:00Z" // latest value, written THIRD
	)

	resetOCITagProbe(ctx, t, driver, cfg.DatabaseName, uid)
	defer resetOCITagProbe(ctx, t, driver, cfg.DatabaseName, uid)

	// Set-once semantics: the value present at node creation (t2) must persist
	// through a later out-of-order earlier value (t1) and a later value (t3).
	for _, observedAt := range []string{t2, t1, t3} {
		if err := upsertOCITagProbe(ctx, driver, cfg.DatabaseName, uid, observedAt); err != nil {
			t.Fatalf("ON CREATE SET upsert (observed_at=%s): %v", observedAt, err)
		}
	}

	first := readOCITagProbeFirst(ctx, t, driver, cfg.DatabaseName, uid)
	if first != t2 {
		t.Errorf("first_observed_at = %q, want first-created %q (ON CREATE SET failed to hold the first value under re-projection)", first, t2)
	}

	// Concurrency robustness: N writers race the same uid; ON CREATE fires only
	// for the single creator, so the node converges to exactly one valid
	// observation with no corruption under NornicDB's concurrent-property-write
	// behavior.
	resetOCITagProbe(ctx, t, driver, cfg.DatabaseName, uid)
	const writers = 8
	stamps := map[string]struct{}{}
	for i := 0; i < writers; i++ {
		stamps[time.Date(2026, 7, 1+i, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)] = struct{}{}
	}
	var wg sync.WaitGroup
	for stamp := range stamps {
		wg.Add(1)
		go func(observedAt string) {
			defer wg.Done()
			_ = upsertOCITagProbe(ctx, driver, cfg.DatabaseName, uid, observedAt)
		}(stamp)
	}
	wg.Wait()
	converged := readOCITagProbeFirst(ctx, t, driver, cfg.DatabaseName, uid)
	if _, ok := stamps[converged]; !ok {
		t.Errorf("under concurrent writers first_observed_at = %q, want one of the written RFC3339 stamps (node corrupted or lost)", converged)
	}
	t.Logf("PROVEN on %s: ON CREATE SET held %s through out-of-order replay; %d concurrent writers converged to %s", cfg.DatabaseName, first, writers, converged)
}

// upsertOCITagProbe runs the ON CREATE SET identity upsert for one observation.
func upsertOCITagProbe(ctx context.Context, driver neo4jdriver.DriverWithContext, database, uid, observedAt string) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	row := map[string]any{
		"uid":             uid,
		"image_ref":       "ghcr.io/eshu-hq/prove:latest",
		"tag":             "latest",
		"resolved_digest": "sha256:" + strings.Repeat("a", 64),
		"observed_at":     observedAt,
	}
	result, err := session.Run(ctx, ociTagFirstObservedOnCreateUpsert, map[string]any{"rows": []any{row}})
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

// resetOCITagProbe removes the probe node so the shim is repeatable.
func resetOCITagProbe(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database, uid string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `MATCH (t:ContainerImageTagObservation {uid: $uid}) DETACH DELETE t`, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("reset probe: %v", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("reset probe consume: %v", err)
	}
}

// readOCITagProbeFirst reads back the first_observed_at value.
func readOCITagProbeFirst(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database, uid string) string {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `MATCH (t:ContainerImageTagObservation {uid: $uid})
RETURN t.first_observed_at AS first`, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("read probe: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("read probe single record: %v", err)
	}
	firstVal, _ := record.Get("first")
	firstStr, _ := firstVal.(string)
	return firstStr
}
