// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// stateVersions maps each state key of the scope to its row version (xmin),
// so a test can tell the rows a link rewrote from the rows it left alone.
func (l *ledgerDB) stateVersions(t *testing.T, scopeID string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range l.queryStrings(t, `
SELECT fact_category || '|' || stable_fact_key || '=' || xmin::text
FROM changed_since_key_state WHERE scope_id = $1`, scopeID) {
		i := strings.LastIndexByte(row, '=')
		out[row[:i]] = row[i+1:]
	}
	return out
}

// TestRebaseOnPrunedPriorWritesOnlyChangedKeys is G1 and G15 on the rebase
// branch (arbiter ruling arb-7127-3d, C2). The state is at x0, x0 is pruned,
// and x1 is full: the writer rebases. The state equals x1's aggregate; only
// the keys an incremental link of the same facts would write are written
// (a root would rewrite every key); the link row is a root with an empty
// prior; no delta or bucket row is written; one prior_pruned break is
// reported. The next link from the rebased state is an ordinary incremental.
func TestRebaseOnPrunedPriorWritesOnlyChangedKeys(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)

	// Reference: the same facts linked incrementally with the prior present.
	l.seedRootedScope(t, w, "ref", "ref0", "ref1")
	if ref := mustLink(t, w, l, "ref"); ref.Kind != linksfreshnessstore.LinkKindIncremental {
		t.Fatalf("reference link = %+v, want incremental", ref)
	}
	wantWritten := l.queryStrings(t, `SELECT fact_category || '|' || stable_fact_key FROM changed_since_link_deltas
WHERE scope_id = 'ref' AND current_state IS NOT NULL ORDER BY 1`)
	wantDeleted := l.queryStrings(t, `SELECT fact_category || '|' || stable_fact_key FROM changed_since_link_deltas
WHERE scope_id = 'ref' AND current_state IS NULL AND prior_state IS NOT NULL ORDER BY 1`)

	l.seedRootedScope(t, w, "rb", "rb0", "rb1")
	before := l.stateVersions(t, "rb")
	l.prune(t, l.ctx)
	if n := l.queryInt(t, `SELECT count(*) FROM scope_generations WHERE generation_id = 'rb0'`); n != 0 {
		t.Fatal("rb0 was not pruned; the rebase branch cannot be reached")
	}

	res := mustLink(t, w, l, "rb")
	if res.Kind != linksfreshnessstore.LinkKindRoot || res.Break != linksfreshnessstore.BreakPriorPruned ||
		res.RebasedFrom != "rb0" || res.GenerationID != "rb1" || res.PriorGenerationID != "" || res.DeltaRows != 0 {
		t.Fatalf("link after the prior's prune = %+v, want a root rebase rb0 -> rb1, prior_pruned, no delta rows", res)
	}
	after := l.stateVersions(t, "rb")
	var written, deleted []string
	for key, version := range after {
		if old, ok := before[key]; !ok || old != version {
			written = append(written, key)
		}
	}
	for key := range before {
		if _, ok := after[key]; !ok {
			deleted = append(deleted, key)
		}
	}
	slices.Sort(written)
	slices.Sort(deleted)
	if !slices.Equal(written, wantWritten) || !slices.Equal(deleted, wantDeleted) {
		t.Fatalf("rebase wrote %v and deleted %v; the incremental reference wrote %v and deleted %v",
			written, deleted, wantWritten, wantDeleted)
	}
	t.Logf("rebase wrote %d and deleted %d state rows; %d before, %d after", len(written), len(deleted), len(before), len(after))
	if len(written) >= len(after) {
		t.Fatalf("rebase rewrote %d of %d state rows; only changed keys may be written", len(written), len(after))
	}
	got, n := l.stateDigest(t, "rb")
	want, wantN := l.aggregateDigest(t, "rb", "rb1")
	if got != want || n != wantN {
		t.Fatalf("state after the rebase = %s (%d keys), aggregate of rb1 = %s (%d keys)", got, n, want, wantN)
	}
	if res.Keys != int64(wantN) {
		t.Fatalf("rebase reported %d keys, aggregate has %d", res.Keys, wantN)
	}
	if n := l.queryInt(t, `SELECT (SELECT count(*) FROM changed_since_link_deltas WHERE scope_id = 'rb')
     + (SELECT count(*) FROM changed_since_link_bucket_counts WHERE scope_id = 'rb')`); n != 0 {
		t.Fatalf("rebase wrote %d delta or bucket rows, want 0", n)
	}
	if rows := l.queryStrings(t, `SELECT generation_id || '|' || prior_generation_id || '|' || link_kind || '|' || delta_rows
FROM changed_since_links WHERE scope_id = 'rb'`); strings.Join(rows, ",") != "rb1||root|0" {
		t.Fatalf("links of rb = %v, want one root rb1 with an empty prior and 0 delta rows", rows)
	}
	if gen, _ := l.cursor(t, "rb"); gen != "rb1" {
		t.Fatalf("cursor state generation = %q, want rb1", gen)
	}
	if links, activations, headless := l.probe(t); links+activations+headless != 0 {
		t.Fatalf("probe = %d/%d/%d after the rebase, want zeros", links, activations, headless)
	}

	// The rebased state is a valid base for the next incremental link.
	l.supersede(t, "rb", "rb1", "rb2")
	l.insertFacts(t, "rb", "rb2", baseFacts())
	l.journal(t, "rb", "rb2", "rb1")
	next := mustLink(t, w, l, "rb")
	if next.Kind != linksfreshnessstore.LinkKindIncremental || next.PriorGenerationID != "rb1" || next.Break != "" {
		t.Fatalf("link after the rebase = %+v, want incremental rb1 -> rb2", next)
	}
	got, n = l.stateDigest(t, "rb")
	want, wantN = l.aggregateDigest(t, "rb", "rb2")
	if got != want || n != wantN {
		t.Fatalf("state after rb2 = %s (%d keys), aggregate of rb2 = %s (%d keys)", got, n, want, wantN)
	}
}

// TestPriorHeldByRetentionIsGenerationLocked is G11 extended to the state
// generation X (arbiter ruling arb-7127-3d, C1): X held FOR UPDATE (the
// retention shape) gives a non-counting generation_locked in under a second,
// writes nothing and leaves the cursor; after the release the link is an
// ordinary incremental from X.
func TestPriorHeldByRetentionIsGenerationLocked(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedRootedScope(t, w, "scope-xl", "xl0", "xl1")
	_, seq := l.cursor(t, "scope-xl")

	holder := l.begin(t)
	defer func() { _ = holder.Rollback() }()
	l.execTx(t, holder, `SELECT 1 FROM scope_generations WHERE generation_id = 'xl0' FOR UPDATE`)
	l.requireGenerationLocked(t, w, "scope-xl")
	if _, now := l.cursor(t, "scope-xl"); now != seq {
		t.Fatalf("cursor moved from %d to %d on generation_locked", seq, now)
	}
	if n := l.queryInt(t, `SELECT attempt_count FROM changed_since_scope_cursor WHERE scope_id = 'scope-xl'`); n != 0 {
		t.Fatalf("generation_locked on the prior counted an attempt (%d)", n)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links WHERE generation_id = 'xl1'`); n != 0 {
		t.Fatalf("generation_locked wrote %d links", n)
	}
	_ = holder.Rollback()
	if got := mustLink(t, w, l, "scope-xl"); got.Kind != linksfreshnessstore.LinkKindIncremental || got.PriorGenerationID != "xl0" {
		t.Fatalf("link after release = %+v, want incremental xl0 -> xl1", got)
	}
}

// TestPriorLockWithACommittedUpdaterDoesNotWait is the #7115 shape (arbiter
// ruling arb-7127-3d, "Why not in PR-3d"): a multixact with a running KEY
// SHARE member and a committed updater, on the prior X and on the activating
// generation G (W4 of arbiter ruling arb-7127-3e-wait). The update commits
// before the lock statement's snapshot, so no chain walk happens: each case
// leaves the row's newest version held by an open transaction, and the
// writer's lock must answer in under a second either way.
func TestPriorLockWithACommittedUpdaterDoesNotWait(t *testing.T) {
	const bump = `UPDATE scope_generations SET ingested_at = ingested_at + interval '1 second' WHERE generation_id = $1`
	for _, target := range []string{"prior", "activating"} {
		for _, tc := range []struct {
			name       string
			keyShare   bool   // a running KEY SHARE member on X
			newest     string // what the open transaction does to X's newest version
			wantLocked bool   // generation_locked, otherwise an incremental link
		}{
			{"key share, committed update, open no-key update (Ack shape)", true, bump, false},
			{
				"key share, committed update, retention skips the carried lock", true,
				`SELECT count(*) FROM (SELECT 1 FROM scope_generations WHERE generation_id = $1 FOR UPDATE SKIP LOCKED) AS got`, false,
			},
			{
				"committed update, retention holds the newest version", false,
				`SELECT 1 FROM scope_generations WHERE generation_id = $1 FOR UPDATE`, true,
			},
		} {
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				gen := map[string]string{"prior": "mx0", "activating": "mx1"}[target]
				l := openLedgerDB(t)
				w := linksfreshnessstore.NewLinkWriter(l.store)
				l.seedRootedScope(t, w, "scope-mx", "mx0", "mx1")
				var open []*sql.Tx
				defer func() {
					for _, tx := range open {
						_ = tx.Rollback()
					}
				}()
				if tc.keyShare {
					member := l.begin(t)
					open = append(open, member)
					l.execTx(t, member, `SELECT 1 FROM scope_generations WHERE generation_id = $1 FOR KEY SHARE`, gen)
				}
				l.exec(t, bump, gen)
				newest := l.begin(t)
				open = append(open, newest)
				if strings.Contains(tc.newest, "SKIP LOCKED") {
					var got int64
					if err := newest.QueryRowContext(l.ctx, tc.newest, gen).Scan(&got); err != nil || got != 0 {
						t.Fatalf("retention lock of %s with a key share carried = %d, %v; want no row", gen, got, err)
					}
				} else {
					l.execTx(t, newest, tc.newest, gen)
				}
				if tc.wantLocked {
					l.requireGenerationLocked(t, w, "scope-mx")
					return
				}
				ctx, cancel := context.WithTimeout(l.ctx, noWait)
				defer cancel()
				start := time.Now()
				got, err := w.LinkNext(ctx, "scope-mx")
				if elapsed := time.Since(start); elapsed > time.Second {
					t.Fatalf("link waited %s on the prior's lock", elapsed)
				}
				if err != nil || got.Kind != linksfreshnessstore.LinkKindIncremental || got.PriorGenerationID != "mx0" {
					t.Fatalf("link = %+v, %v; want incremental mx0 -> mx1", got, err)
				}
			})
		}
	}
}

// TestBackfillEndsTheChainAtAHeldGeneration is C3 of arbiter ruling
// arb-7127-3d: the backfill inserts each activation through its generation
// row FOR KEY SHARE SKIP LOCKED. A generation retention holds inserts
// nothing, without waiting, and ends that scope's chain for the pass; the
// sweeper still journals the active generation.
func TestBackfillEndsTheChainAtAHeldGeneration(t *testing.T) {
	l := openLedgerDB(t)
	t0 := fixtureEpoch
	l.seedScope(t, "bf")
	// The walk starts at the newest activated full generation: b0 full, then
	// deltas b1, b2 and the active b3.
	for i, gen := range []string{"b0", "b1", "b2"} {
		l.seedGeneration(t, "bf", gen, i > 0, "superseded", t0.Add(time.Duration(i)*time.Hour), t0.Add(time.Duration(i+1)*time.Hour))
	}
	l.seedGeneration(t, "bf", "b3", true, "active", t0.Add(3*time.Hour), time.Time{})
	l.setActive(t, "bf", "b3")

	holder := l.begin(t)
	defer func() { _ = holder.Rollback() }()
	l.execTx(t, holder, `SELECT 1 FROM scope_generations WHERE generation_id = 'b1' FOR UPDATE`)
	ctx, cancel := context.WithTimeout(l.ctx, noWait)
	defer cancel()
	start := time.Now()
	result, err := linksfreshnessstore.NewJournalStore(l.store).Journal(ctx, 10)
	if err != nil {
		t.Fatalf("Journal: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Journal waited %s on the held generation", elapsed)
	}
	want := []string{"bf|b0|<null>|backfill", "bf|b3|<null>|sweeper"}
	if got := l.journalRows(t); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("journal = %v, want %v (the chain ends at the held b1)", got, want)
	}
	if result.BackfillRows != 1 || result.BackfillScopes != 1 {
		t.Fatalf("Journal = %+v, want one backfill row in one scope", result)
	}
}

// TestBackfillInsertFencesItsGeneration runs the backfill's insert directly:
// for a pruned generation it inserts nothing, and a row it inserts keeps its
// generation FOR KEY SHARE until the pass commits, so retention's FOR UPDATE
// SKIP LOCKED skips that generation (C3).
func TestBackfillInsertFencesItsGeneration(t *testing.T) {
	l := openLedgerDB(t)
	l.seedScope(t, "bi")
	l.seedGeneration(t, "bi", "bi0", false, "active", fixtureEpoch, time.Time{})
	pass := l.begin(t)
	defer func() { _ = pass.Rollback() }()
	insert := func(gen string) int64 {
		t.Helper()
		res, err := pass.ExecContext(l.ctx, linksfreshnessstore.InsertBackfillActivationQueryForTest,
			"bi", gen, "", linksfreshnessstore.SourceBackfill, fixtureEpoch)
		if err != nil {
			t.Fatalf("insert %s: %v", gen, err)
		}
		n, _ := res.RowsAffected()
		return n
	}
	if n := insert("bi-pruned"); n != 0 {
		t.Fatalf("backfill insert of a pruned generation inserted %d rows, want 0", n)
	}
	if n := insert("bi0"); n != 1 {
		t.Fatalf("backfill insert of bi0 inserted %d rows, want 1", n)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM (SELECT 1 FROM scope_generations WHERE generation_id = 'bi0'
FOR UPDATE SKIP LOCKED) AS got`); n != 0 {
		t.Fatal("retention could lock bi0 while the pass that journaled it is open")
	}
}

// TestOrphansCountsPlantedRows runs the orphan probe (C5) over planted rows:
// a link with a pruned generation, a link with a pruned prior, an activation
// of a pruned generation and a bucket group with no link.
func TestOrphansCountsPlantedRows(t *testing.T) {
	l := openLedgerDB(t)
	l.seedScope(t, "op")
	l.seedGeneration(t, "op", "op0", false, "active", fixtureEpoch, time.Time{})
	for _, s := range []string{
		`INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version,
    delta_rows, files_keys, content_entities_keys, facts_keys, computed_at)
 VALUES ('op', 'gone', '', 'root', 1, 0, 0, 0, 0, now()), ('op', 'op0', 'gone-prior', 'incremental', 1, 0, 0, 0, 0, now()),
        ('op', 'op0', '', 'root', 1, 0, 0, 0, 0, now())`,
		`INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
 VALUES ('op', 'gone', NULL, 'ack', now()), ('op', 'op0', 'gone-prior', 'ack', now())`,
		`INSERT INTO changed_since_link_bucket_counts (scope_id, generation_id, prior_generation_id, fact_category,
    classification, key_count)
 VALUES ('op', 'op0', 'headless', 'files', 'added', 1), ('op', 'op0', 'gone-prior', 'files', 'added', 1)`,
	} {
		l.exec(t, s)
	}
	got, err := linksfreshnessstore.NewJournalStore(l.store).Orphans(l.ctx)
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	want := linksfreshnessstore.LedgerOrphans{Links: 2, Activations: 1, HeadlessBucketGroups: 1}
	if got != want {
		t.Fatalf("Orphans = %+v, want %+v", got, want)
	}
}

func (l *ledgerDB) begin(t *testing.T) *sql.Tx {
	t.Helper()
	tx, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	return tx
}

func (l *ledgerDB) execTx(t *testing.T, tx *sql.Tx, query string, args ...any) {
	t.Helper()
	if _, err := tx.ExecContext(l.ctx, query, args...); err != nil {
		t.Fatalf("exec %q: %v", firstLine(query), err)
	}
}

// requireGenerationLocked links scopeID and requires a non-counting
// generation_locked within a second.
func (l *ledgerDB) requireGenerationLocked(t *testing.T, w *linksfreshnessstore.LinkWriter, scopeID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(l.ctx, noWait)
	defer cancel()
	start := time.Now()
	_, err := w.LinkNext(ctx, scopeID)
	if reason, ok := linksfreshnessstore.RetryReasonOf(err); !ok || reason != linksfreshnessstore.RetryGenerationLocked {
		t.Fatalf("LinkNext(%s) = %v, want generation_locked", scopeID, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("generation_locked took %s, want a non-blocking miss", elapsed)
	}
}
