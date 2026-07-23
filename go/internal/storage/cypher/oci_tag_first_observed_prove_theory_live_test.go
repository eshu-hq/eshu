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

// ociTagFirstObservedIdentityUpsert and ociTagFirstObservedSetOnce are the exact
// TWO-STATEMENT set-once shape the #5459 tag-observation writer adopts for
// first_observed_at, the first queryable node-property timestamp in the
// canonical graph.
//
// The ContainerImageTagObservation node dedups every observation of a
// (repository_id, tag, resolved_digest) into ONE node keyed by a stable uid, so
// the timestamp on that node is an aggregate over repeated observations. Three
// NornicDB realities force the shape (all disproven live before this shim
// settled):
//
//  1. A compound self-referencing guard
//     (SET t.x = CASE WHEN t.x IS NULL OR row.v < t.x THEN row.v ELSE t.x END)
//     inside UNWIND...MERGE...SET does NOT evaluate the self-read on the pinned
//     NornicDB binary — it stored the literal token and regressed to
//     last-write-wins.
//  2. coalesce(t.x, row.v) inside the SAME UNWIND...MERGE...SET also regressed
//     to last-write-wins: within a single UNWIND+MERGE statement the MERGE
//     binding shadows the persisted property as null, so every self-read sees
//     null. (A separate-statement coalesce over an already-committed node reads
//     correctly, but the writer's upsert is one fused statement.)
//  3. Concurrent property writes to the same node lose updates on NornicDB
//     (issue #5062, ~26% loss measured for the owner ledger), so any
//     read-modify-write min-guard is unsafe unless same-uid writes are
//     serialized — which the OCI projection does not guarantee.
//
// The two-statement set-once shape sidesteps all three: statement 1 is the
// ordinary identity MERGE (no timestamp self-read); statement 2 MATCHes the
// now-committed node and sets first_observed_at ONLY WHERE it is still null. The
// MATCH reads the persisted value (no MERGE shadow), the WHERE...IS NULL makes
// it idempotent (every projection after the first is a no-op that never
// regresses the value), and it is concurrency-robust by construction — once any
// writer commits a non-null value every later write is filtered out, so a lost
// concurrent write only drops a redundant no-op and the survivor is a valid
// earliest observation. Under the reducer's in-order generation processing the
// first projected observation of a digest is the chronologically earliest, so
// first-written == first-observed. A back-dated observation arriving after a
// later one is a documented limitation, tracked as a follow-up alongside
// last_observed_at and true per-event history (which need a transition-keyed uid
// and same-uid write serialization).
const ociTagFirstObservedIdentityUpsert = `UNWIND $rows AS row
MERGE (t:ContainerImageTagObservation:OciImageTagObservation {uid: row.uid})
SET t.image_ref = row.image_ref,
    t.tag = row.tag,
    t.resolved_digest = row.resolved_digest`

const ociTagFirstObservedSetOnce = `UNWIND $rows AS row
MATCH (t:ContainerImageTagObservation {uid: row.uid})
WHERE t.first_observed_at IS NULL
SET t.first_observed_at = row.observed_at`

// TestLiveOCITagFirstObservedProveTheory proves, against a live NornicDB, that
// the #5459 two-statement set-once holds first_observed_at at the FIRST projected
// observation and never regresses it under later or out-of-order re-projection.
// It upserts the same uid three times — t2 (first), then an earlier t1, then a
// later t3 — and asserts first_observed_at stayed at t2 (first written). A bare
// SET would leave it at t3; a self-read inside UNWIND+MERGE leaves last-write or
// null. Only the separate-MATCH set-once holds the first value, which is the accuracy
// contract the ordered read surface depends on.
//
// Skipped unless ESHU_OCI_TAG_PROVE_LIVE=1 with the Common Compose Environment
// (NEO4J_URI/NEO4J_USERNAME/NEO4J_PASSWORD/ESHU_NEO4J_DATABASE) at a live
// NornicDB.
func TestLiveOCITagFirstObservedProveTheory(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ociTagFirstObservedProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5459 tag-observation two-statement set-once prove-theory shim", ociTagFirstObservedProveEnv)
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

	// Set-once semantics: the first write (t2) must persist through a later
	// out-of-order earlier value (t1) and a later value (t3).
	for _, observedAt := range []string{t2, t1, t3} {
		if err := upsertOCITagProbe(ctx, driver, cfg.DatabaseName, uid, observedAt); err != nil {
			t.Fatalf("set-once upsert (observed_at=%s): %v", observedAt, err)
		}
	}

	first := readOCITagProbeFirst(ctx, t, driver, cfg.DatabaseName, uid)
	if first != t2 {
		t.Errorf("first_observed_at = %q, want first-written %q (set-once failed to hold the first value under re-projection)", first, t2)
	}

	// Concurrency robustness: N writers race the same uid; the set-once is a
	// no-op after the first commit, so the node must converge to exactly one
	// valid observation with no corruption or null loss under NornicDB's
	// concurrent-property-write behavior.
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
	t.Logf("PROVEN on %s: set-once held %s through out-of-order replay; %d concurrent writers converged to %s", cfg.DatabaseName, first, writers, converged)
}

// upsertOCITagProbe runs the two-statement set-once upsert for one observation.
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
	rows := map[string]any{"rows": []any{row}}
	identity, err := session.Run(ctx, ociTagFirstObservedIdentityUpsert, rows)
	if err != nil {
		return err
	}
	if _, err := identity.Consume(ctx); err != nil {
		return err
	}
	setOnce, err := session.Run(ctx, ociTagFirstObservedSetOnce, rows)
	if err != nil {
		return err
	}
	_, err = setOnce.Consume(ctx)
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

// readOCITagProbeFirst reads back the coalesced first_observed_at value.
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
