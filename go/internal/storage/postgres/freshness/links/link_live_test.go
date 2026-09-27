// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// stateDigest hashes the scope's state rows in key order.
func (l *ledgerDB) stateDigest(t *testing.T, scopeID string) (string, int) {
	t.Helper()
	lines := l.queryStrings(t, `
SELECT fact_category || '|' || stable_fact_key || '|' || fact_kind || '|' || encode(state, 'hex')
       || '|' || COALESCE(owner_uri, '')
FROM changed_since_key_state WHERE scope_id = $1 ORDER BY 1`, scopeID)
	return hashLines(lines), len(lines)
}

// aggregateDigest is the oracle for a state: the effective keys of one full
// generation, built independently of the writer's statement (a WHERE on
// is_tombstone instead of FILTER, array_agg instead of string_agg).
func (l *ledgerDB) aggregateDigest(t *testing.T, scopeID, generationID string) (string, int) {
	t.Helper()
	lines := l.queryStrings(t, `
SELECT cat || '|' || stable_fact_key || '|' || kind || '|' || encode(state, 'hex') || '|' || COALESCE(owner, '')
FROM (
    SELECT cat, stable_fact_key, MIN(fact_kind) AS kind, MIN(source_uri) AS owner,
           sha256(decode(array_to_string(array_agg(encode(h, 'hex') ORDER BY h), ''), 'hex')) AS state
    FROM (
        SELECT CASE fact_kind WHEN 'file' THEN 'files' WHEN 'content_entity' THEN 'content_entities'
                    ELSE 'facts' END AS cat,
               stable_fact_key, fact_kind, source_uri,
               sha256(convert_to((`+linksfreshnessstore.PayloadDigestInput+`)::text, 'UTF8')) AS h
        FROM fact_records
        WHERE scope_id = $1 AND generation_id = $2 AND NOT is_tombstone
          AND `+linksfreshnessstore.ExcludeReducerDerivedKinds+`
    ) AS rows
    GROUP BY cat, stable_fact_key
) AS keyed
ORDER BY 1`, scopeID, generationID)
	return hashLines(lines), len(lines)
}

func hashLines(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func (l *ledgerDB) cursor(t *testing.T, scopeID string) (string, int64) {
	t.Helper()
	var gen string
	var seq int64
	if err := l.raw.QueryRowContext(l.ctx, `
SELECT COALESCE(state_generation_id, ''), state_activation_seq
FROM changed_since_scope_cursor WHERE scope_id = $1`, scopeID).Scan(&gen, &seq); err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	return gen, seq
}

// baseFacts is a small full generation exercising every classification.
func baseFacts() []fact {
	return []fact{
		{kind: "file", key: "file:a.go", payload: `{"path":"a.go","sha":"1"}`, uri: "a.go"},
		{kind: "file", key: "file:b.go", payload: `{"path":"b.go","sha":"1"}`, uri: "b.go"},
		{kind: "file", key: "file:gone.go", payload: `{"path":"gone.go"}`, uri: "gone.go"},
		{kind: "file", key: "file:dropped.go", payload: `{"path":"dropped.go"}`, uri: "dropped.go"},
		{kind: "content_entity", key: "ent:a.F", payload: `{"name":"F","indexed_at":"t0"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:b.G", payload: `{"name":"G","indexed_at":"t0"}`, uri: "b.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":1,"indexed_at":"t0"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":2,"indexed_at":"t0"}`, uri: "a.go"},
		{kind: "repository", key: "repo", payload: `{"name":"r"}`},
		{kind: "kind_a", key: "kindchange", payload: `{"x":1}`},
		{kind: "reducer_thing", key: "reducer_only", payload: `{"x":1}`},
	}
}

// nextFacts changes baseFacts: an update, an indexed_at-only change, a
// changed non-minimum duplicate payload, a tombstone, a drop, an add and a
// kind change.
func nextFacts() []fact {
	return []fact{
		{kind: "file", key: "file:a.go", payload: `{"path":"a.go","sha":"2"}`, uri: "a.go"},
		{kind: "file", key: "file:b.go", payload: `{"path":"b.go","sha":"1"}`, uri: "b.go"},
		{kind: "file", key: "file:gone.go", tombstone: true, uri: "gone.go"},
		{kind: "file", key: "file:new.go", payload: `{"path":"new.go"}`, uri: "new.go"},
		{kind: "file", key: "file:never.go", tombstone: true, uri: "never.go"},
		{kind: "content_entity", key: "ent:a.F", payload: `{"name":"F","indexed_at":"t1"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:b.G", payload: `{"name":"G2","indexed_at":"t1"}`, uri: "b.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":1,"indexed_at":"t1"}`, uri: "a.go"},
		{kind: "content_entity", key: "ent:dup", payload: `{"v":3,"indexed_at":"t1"}`, uri: "a.go"},
		{kind: "repository", key: "repo", payload: `{"name":"r"}`},
		{kind: "kind_b", key: "kindchange", payload: `{"x":1}`},
		{kind: "reducer_thing", key: "reducer_only", payload: `{"x":2}`},
	}
}

func mustLink(t *testing.T, w *linksfreshnessstore.LinkWriter, l *ledgerDB, scopeID string) linksfreshnessstore.LinkResult {
	t.Helper()
	result, err := w.LinkNext(l.ctx, scopeID)
	if err != nil {
		t.Fatalf("LinkNext(%s): %v", scopeID, err)
	}
	return result
}

func TestLinkRootThenIncrementalStateEqualsAggregate(t *testing.T) {
	l := openLedgerDB(t)
	const scope = "scope-1"
	l.seedScope(t, scope)
	l.seedGeneration(t, scope, "g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, scope, "g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
	l.insertFacts(t, scope, "g0", baseFacts())
	l.insertFacts(t, scope, "g1", nextFacts())
	l.journal(t, scope, "g0", "")
	l.journal(t, scope, "g1", "g0")
	w := linksfreshnessstore.NewLinkWriter(l.store)

	root := mustLink(t, w, l, scope)
	if root.Kind != linksfreshnessstore.LinkKindRoot || root.GenerationID != "g0" || root.Break != "" {
		t.Fatalf("first link = %+v, want root of g0", root)
	}
	if got, n := l.stateDigest(t, scope); true {
		want, wantN := l.aggregateDigest(t, scope, "g0")
		if got != want || n != wantN {
			t.Fatalf("state after root = %s (%d keys), aggregate of g0 = %s (%d keys)", got, n, want, wantN)
		}
	}

	inc := mustLink(t, w, l, scope)
	if inc.Kind != linksfreshnessstore.LinkKindIncremental || inc.PriorGenerationID != "g0" {
		t.Fatalf("second link = %+v, want incremental g0 -> g1", inc)
	}
	got, n := l.stateDigest(t, scope)
	want, wantN := l.aggregateDigest(t, scope, "g1")
	if got != want || n != wantN {
		t.Fatalf("state after incremental = %s (%d keys), aggregate of g1 = %s (%d keys)", got, n, want, wantN)
	}
	if gen, _ := l.cursor(t, scope); gen != "g1" {
		t.Fatalf("cursor state generation = %q, want g1", gen)
	}
	buckets := l.queryStrings(t, `
SELECT fact_category || '/' || classification || '=' || key_count
FROM changed_since_link_bucket_counts WHERE scope_id = $1 ORDER BY 1`, scope)
	wantBuckets := []string{
		"content_entities/updated=2", "facts/unchanged=1",
		"files/added=1", "files/dropped=1", "files/retired=1", "files/superseded=1", "files/updated=1",
	}
	if strings.Join(buckets, ",") != strings.Join(wantBuckets, ",") {
		t.Fatalf("buckets = %v, want %v", buckets, wantBuckets)
	}
	if idle := mustLink(t, w, l, scope); !idle.Idle {
		t.Fatalf("third call = %+v, want idle", idle)
	}
}

// TestChainBreakKeepsStateThenIncremental is gate G15 (#7127 ruling 8.5).
func TestChainBreakKeepsStateThenIncremental(t *testing.T) {
	l := openLedgerDB(t)
	const scope = "scope-break"
	l.seedScope(t, scope)
	l.seedGeneration(t, scope, "f0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, scope, "d1", true, "superseded", fixtureEpoch.Add(time.Hour), fixtureEpoch.Add(2*time.Hour))
	l.seedGeneration(t, scope, "d2", true, "superseded", fixtureEpoch.Add(2*time.Hour), fixtureEpoch.Add(3*time.Hour))
	l.seedGeneration(t, scope, "f3", false, "active", fixtureEpoch.Add(3*time.Hour), time.Time{})
	l.insertFacts(t, scope, "f0", baseFacts())
	l.insertFacts(t, scope, "d1", []fact{{kind: "file", key: "file:a.go", payload: `{"x":9}`, uri: "a.go"}})
	l.insertFacts(t, scope, "d2", []fact{{kind: "file", key: "file:b.go", payload: `{"x":9}`, uri: "b.go"}})
	l.insertFacts(t, scope, "f3", nextFacts())
	l.journal(t, scope, "f0", "")
	l.journal(t, scope, "d1", "f0") // prior matches the state: overlay would apply
	l.journal(t, scope, "d2", "")   // unknown prior (sweeper shape)
	l.journal(t, scope, "f3", "d2")
	w := linksfreshnessstore.NewLinkWriter(l.store)

	mustLink(t, w, l, scope)
	rootDigest, _ := l.stateDigest(t, scope)
	for _, want := range []linksfreshnessstore.BreakReason{
		linksfreshnessstore.BreakOverlayUnproven, linksfreshnessstore.BreakPriorMismatch,
	} {
		got := mustLink(t, w, l, scope)
		if got.Break != want || got.Kind != linksfreshnessstore.LinkKindNone {
			t.Fatalf("delta link = %+v, want break %s", got, want)
		}
		if gen, _ := l.cursor(t, scope); gen != "f0" {
			t.Fatalf("break moved the state generation to %q, want f0 kept", gen)
		}
		if d, _ := l.stateDigest(t, scope); d != rootDigest {
			t.Fatalf("break changed the state rows")
		}
	}
	inc := mustLink(t, w, l, scope)
	if inc.Kind != linksfreshnessstore.LinkKindIncremental || inc.PriorGenerationID != "f0" {
		t.Fatalf("link after break = %+v, want incremental f0 -> f3", inc)
	}
	got, n := l.stateDigest(t, scope)
	want, wantN := l.aggregateDigest(t, scope, "f3")
	if got != want || n != wantN {
		t.Fatalf("state after break then full = %s (%d), aggregate of f3 = %s (%d)", got, n, want, wantN)
	}
	// Only the changed keys were written: the link's deltas, not the scope.
	if deltas := l.queryInt(t, `SELECT delta_rows FROM changed_since_links WHERE generation_id = 'f3'`); deltas != 8 {
		t.Fatalf("incremental after break wrote %d delta rows, want 8", deltas)
	}
}

func TestDeltaWithoutRootAndPrunedBeforeLink(t *testing.T) {
	l := openLedgerDB(t)
	const scope = "scope-noroot"
	l.seedScope(t, scope)
	l.seedGeneration(t, scope, "d0", true, "active", fixtureEpoch, time.Time{})
	l.journal(t, scope, "d0", "")
	l.journal(t, scope, "pruned", "d0") // no scope_generations row
	w := linksfreshnessstore.NewLinkWriter(l.store)
	if got := mustLink(t, w, l, scope); got.Break != linksfreshnessstore.BreakDeltaWithoutRoot {
		t.Fatalf("delta without state = %+v, want delta_without_root", got)
	}
	if got := mustLink(t, w, l, scope); got.Break != linksfreshnessstore.BreakPrunedBeforeLink {
		t.Fatalf("absent generation = %+v, want pruned_before_link", got)
	}
	gen, seq := l.cursor(t, scope)
	if gen != "" || seq != 2 {
		t.Fatalf("cursor = (%q, %d), want (\"\", 2)", gen, seq)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links`); n != 0 {
		t.Fatalf("breaks wrote %d links, want 0", n)
	}
}

// TestGenerationLockedIsRetryable is gate G11: a generation row held FOR
// UPDATE (the retention shape) gives generation_locked and leaves the cursor.
func TestGenerationLockedIsRetryable(t *testing.T) {
	l := openLedgerDB(t)
	const scope = "scope-genlock"
	l.seedScope(t, scope)
	l.seedGeneration(t, scope, "g0", false, "active", fixtureEpoch, time.Time{})
	l.insertFacts(t, scope, "g0", baseFacts())
	l.journal(t, scope, "g0", "")
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(l.ctx, `SELECT 1 FROM scope_generations WHERE generation_id = 'g0' FOR UPDATE`); err != nil {
		t.Fatalf("hold generation: %v", err)
	}
	w := linksfreshnessstore.NewLinkWriter(l.store)
	start := time.Now()
	_, err = w.LinkNext(l.ctx, scope)
	if reason, ok := linksfreshnessstore.RetryReasonOf(err); !ok || reason != linksfreshnessstore.RetryGenerationLocked {
		t.Fatalf("LinkNext with generation held = %v, want generation_locked", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("generation_locked took %s, want a non-blocking miss", elapsed)
	}
	if _, seq := l.cursor(t, scope); seq != 0 {
		t.Fatalf("cursor moved to %d on a retry", seq)
	}
	if n := l.queryInt(t, `SELECT attempt_count FROM changed_since_scope_cursor WHERE scope_id = $1`, scope); n != 0 {
		t.Fatalf("generation_locked counted an attempt (%d); it is non-counting", n)
	}
	_ = holder.Rollback()
	if got := mustLink(t, w, l, scope); got.Kind != linksfreshnessstore.LinkKindRoot {
		t.Fatalf("retry after release = %+v, want root", got)
	}
}

// TestSlotBusyBlocksFullLinksOnly is gate G12.
func TestSlotBusyBlocksFullLinksOnly(t *testing.T) {
	l := openLedgerDB(t)
	l.seedScope(t, "full")
	l.seedGeneration(t, "full", "f0", false, "active", fixtureEpoch, time.Time{})
	l.insertFacts(t, "full", "f0", baseFacts())
	l.journal(t, "full", "f0", "")
	l.seedScope(t, "delta")
	l.seedGeneration(t, "delta", "d0", true, "active", fixtureEpoch, time.Time{})
	l.journal(t, "delta", "d0", "")

	w := linksfreshnessstore.NewLinkWriter(l.store)
	w.Slots = 2
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	for slot := 1; slot <= w.Slots; slot++ {
		var got bool
		if err := holder.QueryRowContext(l.ctx, `SELECT pg_try_advisory_xact_lock($1::integer, $2::integer)`,
			linksfreshnessstore.SlotLockClass, slot).Scan(&got); err != nil || !got {
			t.Fatalf("hold slot %d: got=%v err=%v", slot, got, err)
		}
	}
	_, err = w.LinkNext(l.ctx, "full")
	if reason, ok := linksfreshnessstore.RetryReasonOf(err); !ok || reason != linksfreshnessstore.RetrySlotBusy {
		t.Fatalf("full link with every slot held = %v, want slot_busy", err)
	}
	if got := mustLink(t, w, l, "delta"); got.Break != linksfreshnessstore.BreakDeltaWithoutRoot {
		t.Fatalf("delta activation with slots held = %+v, want its break unaffected", got)
	}
	_ = holder.Rollback()
	if got := mustLink(t, w, l, "full"); got.Kind != linksfreshnessstore.LinkKindRoot {
		t.Fatalf("full link after release = %+v, want root", got)
	}
}

func TestRetryErrorIsRetryable(t *testing.T) {
	var err error = &linksfreshnessstore.RetryError{Reason: linksfreshnessstore.RetrySlotBusy}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("RetryError does not satisfy Retryable")
	}
}
