// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// noWait bounds a step that must not wait on the other transaction. Every
// statement involved takes milliseconds on these fixtures; a lock wait would
// run to the bound.
const noWait = 10 * time.Second

// prune runs one retention batch with the test policy.
func (l *ledgerDB) prune(t *testing.T, ctx context.Context) postgres.GenerationRetentionResult {
	t.Helper()
	result, err := postgres.NewGenerationRetentionStore(l.store).PruneSupersededGenerations(ctx, prunePolicy(1_000_000))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations: %v", err)
	}
	return result
}

// rowsNaming counts ledger rows ruling 2.8 retires with gens.
func (l *ledgerDB) rowsNaming(t *testing.T, gens ...string) int {
	t.Helper()
	n := 0
	for _, row := range l.ledgerSnapshot(t) {
		if namesGeneration(row, gens) {
			n++
		}
	}
	return n
}

// supersede makes gen superseded (old enough to prune) and next the active
// generation, so a later batch prunes gen.
func (l *ledgerDB) supersede(t *testing.T, scopeID, gen, next string) {
	t.Helper()
	l.exec(t, `UPDATE scope_generations SET status = 'superseded', superseded_at = $2 WHERE generation_id = $1`,
		gen, fixtureEpoch.Add(-24*time.Hour))
	l.seedGeneration(t, scopeID, next, false, "active", fixtureEpoch.Add(10*time.Hour), time.Time{})
	l.setActive(t, scopeID, next)
}

// seedRootedScope seeds x0 (superseded, prunable) linked as the scope's root
// and x1 (active) journaled with prior x0 and not yet linked: the state
// generation of the scope is x0.
func (l *ledgerDB) seedRootedScope(t *testing.T, w *linksfreshnessstore.LinkWriter, scopeID, x0, x1 string) {
	t.Helper()
	l.seedChain(t, scopeID, []string{x0, x1}, fixtureEpoch.Add(-48*time.Hour))
	if root := mustLink(t, w, l, scopeID); root.Kind != linksfreshnessstore.LinkKindRoot {
		t.Fatalf("%s: first link = %+v, want root of %s", scopeID, root, x0)
	}
}

// TestRetentionDoesNotWaitOnALinkInFlight is the insert-then-delete half of
// the race, with a hand-written link transaction that locks only the
// activating generation, as the writer did before PR-3e: it proves the ledger
// delete statement itself keeps a late link whole, whatever a writer locks.
// The link has inserted (x1 -> x0) rows, uncommitted, when retention prunes x0
// (the link's prior, which this transaction does not lock; the real writer
// now does, see TestRetentionRacesLinkWriter). Retention does not wait: it
// cannot see the uncommitted rows and deletes only the committed ones. The
// link then commits with a pruned prior. That row is deleted by the batch that
// prunes x1, so the ledger converges.
func TestRetentionDoesNotWaitOnALinkInFlight(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedRootedScope(t, w, "scope-r", "r0", "r1")

	link, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin link: %v", err)
	}
	defer func() { _ = link.Rollback() }()
	for _, s := range []string{
		`SELECT 1 FROM scope_generations WHERE generation_id = 'r1' FOR KEY SHARE SKIP LOCKED`,
		`INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version,
		    delta_rows, files_keys, content_entities_keys, facts_keys, computed_at)
		 VALUES ('scope-r', 'r1', 'r0', 'incremental', 1, 1, 0, 1, 0, now())`,
		`INSERT INTO changed_since_link_deltas (scope_id, generation_id, prior_generation_id, fact_category,
		    classification, stable_fact_key, current_tombstoned)
		 VALUES ('scope-r', 'r1', 'r0', 'content_entities', 'added', 'ent:x', FALSE)`,
		`INSERT INTO changed_since_link_bucket_counts (scope_id, generation_id, prior_generation_id, fact_category,
		    classification, key_count)
		 VALUES ('scope-r', 'r1', 'r0', 'content_entities', 'added', 1)`,
	} {
		if _, err := link.ExecContext(l.ctx, s); err != nil {
			t.Fatalf("link step %q: %v", firstLine(s), err)
		}
	}

	ctx, cancel := context.WithTimeout(l.ctx, noWait)
	defer cancel()
	start := time.Now()
	if result := l.prune(t, ctx); result.GenerationsPruned != 1 {
		t.Fatalf("pruned %d generations, want 1 (r0)", result.GenerationsPruned)
	}
	t.Logf("retention finished in %s with a link in flight", time.Since(start))
	if err := link.Commit(); err != nil {
		t.Fatalf("commit link: %v", err)
	}
	if n := l.rowsNaming(t, "r0"); n != 3 {
		t.Fatalf("%d rows name r0 after the race, want 3 (the link committed after the prune)", n)
	}
	// P2 (b): the late link survives whole: no delta or bucket row without its link.
	if n := l.headlessDeltas(t); n != 0 {
		t.Fatalf("%d delta rows have no link row after the race", n)
	}
	if links, _, headless := l.probe(t); links != 1 || headless != 0 {
		t.Fatalf("probe = %d orphan links, %d headless bucket groups; want 1 and 0", links, headless)
	}

	l.supersede(t, "scope-r", "r1", "r2")
	l.prune(t, l.ctx)
	if n := l.rowsNaming(t, "r0", "r1"); n != 0 {
		t.Fatalf("%d ledger rows still name r0 or r1 after r1 was pruned", n)
	}
}

// TestLinkWriterDoesNotWaitOnRetentionInFlight is the delete-then-insert half.
// A retention transaction holds the scope and x0 FOR UPDATE and has deleted
// x0's ledger rows and generation row, uncommitted, when the real link writer
// links x1 from the state at x0. The writer fences the prior (PR-3e, C1): x0's
// row is still visible but locked, so the writer returns generation_locked
// without waiting and writes nothing. Once retention commits, the next link
// finds x0 gone and rebases (C2); no link ever names the pruned x0.
func TestLinkWriterDoesNotWaitOnRetentionInFlight(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedRootedScope(t, w, "scope-w", "w0", "w1")

	retention, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin retention: %v", err)
	}
	defer func() { _ = retention.Rollback() }()
	for _, s := range []string{
		`SELECT 1 FROM ingestion_scopes WHERE scope_id = 'scope-w' FOR UPDATE`,
		`SELECT 1 FROM scope_generations WHERE generation_id = 'w0' FOR UPDATE`,
	} {
		if _, err := retention.ExecContext(l.ctx, s); err != nil {
			t.Fatalf("retention step %q: %v", s, err)
		}
	}
	deleted, err := linksfreshnessstore.DeletePrunedGenerationRows(l.ctx, postgres.SQLTx{Tx: retention}, []string{"scope-w"}, []string{"w0"})
	if err != nil {
		t.Fatalf("DeletePrunedGenerationRows: %v", err)
	}
	if deleted[linksfreshnessstore.TableLinks] != 1 || deleted[linksfreshnessstore.TableActivations] != 1 {
		t.Fatalf("deleted %v, want the root link and w0's activation", deleted)
	}
	if _, err := retention.ExecContext(l.ctx, `DELETE FROM scope_generations WHERE generation_id = 'w0'`); err != nil {
		t.Fatalf("delete w0: %v", err)
	}

	l.requireGenerationLocked(t, w, "scope-w")
	if err := retention.Commit(); err != nil {
		t.Fatalf("commit retention: %v", err)
	}
	result := mustLink(t, w, l, "scope-w")
	if result.Kind != linksfreshnessstore.LinkKindRoot || result.Break != linksfreshnessstore.BreakPriorPruned || result.RebasedFrom != "w0" {
		t.Fatalf("link after retention committed = %+v, want a root rebase w0 -> w1", result)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links WHERE prior_generation_id = 'w0'`); n != 0 {
		t.Fatalf("%d links with prior w0 after the race, want 0", n)
	}
	if links, activations, headless := l.probe(t); links+activations+headless != 0 {
		t.Fatalf("probe = %d/%d/%d, want zeros", links, activations, headless)
	}

	l.supersede(t, "scope-w", "w1", "w2")
	l.prune(t, l.ctx)
	if n := l.rowsNaming(t, "w0", "w1"); n != 0 {
		t.Fatalf("%d ledger rows still name w0 or w1 after w1 was pruned", n)
	}
}

// TestRetentionSkipsTheGenerationALinkHolds proves the writer's FOR KEY SHARE
// on the activating generation keeps retention off it: the candidate lock is
// FOR UPDATE SKIP LOCKED, so the generation and its ledger rows stay until the
// link transaction ends, and the next batch prunes them.
func TestRetentionSkipsTheGenerationALinkHolds(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.linkChain(t, w, "scope-k", []string{"k0", "k1"}, fixtureEpoch.Add(-48*time.Hour))
	before := l.rowsNaming(t, "k0")

	link, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin link: %v", err)
	}
	defer func() { _ = link.Rollback() }()
	if _, err := link.ExecContext(l.ctx, `SELECT 1 FROM scope_generations WHERE generation_id = 'k0' FOR KEY SHARE`); err != nil {
		t.Fatalf("hold k0: %v", err)
	}
	ctx, cancel := context.WithTimeout(l.ctx, noWait)
	defer cancel()
	if result := l.prune(t, ctx); result.GenerationsPruned != 0 {
		t.Fatalf("pruned %d generations while k0 was held, want 0", result.GenerationsPruned)
	}
	if n := l.rowsNaming(t, "k0"); n != before {
		t.Fatalf("%d rows name k0 while held, want %d", n, before)
	}
	_ = link.Rollback()
	if result := l.prune(t, l.ctx); result.GenerationsPruned != 1 {
		t.Fatalf("pruned %d generations after release, want 1", result.GenerationsPruned)
	}
	if n := l.rowsNaming(t, "k0"); n != 0 {
		t.Fatalf("%d rows name k0 after its prune", n)
	}
}

// TestRetentionRacesLinkWriter runs the real link writer and a real retention
// batch at once on the link's prior x0, 24 times on fresh scopes, each from
// its own pooled session. Neither may fail or wait out the bound. Each race
// ends one of the ways PR-3e allows (arbiter ruling arb-7127-3d, "FOR THE
// COORDINATOR TO FILE" item 1): retention skipped x0 because the link held it
// (skip), the link committed first and retention then pruned x0 with its link
// (link_then_prune), retention held x0 and the link got generation_locked
// (then the retry rebases), or retention pruned x0 first and the link rebased.
// Never an orphan: the probe is zero after every race, and no delta or bucket
// row is without its link.
func TestRetentionRacesLinkWriter(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	const races = 24
	outcomes := map[string]int{}
	for i := range races {
		scope, x0, x1 := fmt.Sprintf("race-%02d", i), fmt.Sprintf("x%02d-0", i), fmt.Sprintf("x%02d-1", i)
		l.seedRootedScope(t, w, scope, x0, x1)
		ctx, cancel := context.WithTimeout(l.ctx, noWait)
		var wg sync.WaitGroup
		var linkErr, pruneErr error
		var linked linksfreshnessstore.LinkResult
		// Stagger the starts so both orders happen: even races delay the link,
		// odd races delay the prune, by 0 to 14 ms.
		linkDelay, pruneDelay := raceOffset(i), time.Duration(0)
		if i%2 == 1 {
			linkDelay, pruneDelay = 0, raceOffset(i)
		}
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			time.Sleep(linkDelay)
			linked, linkErr = w.LinkNext(ctx, scope)
		}()
		go func() {
			defer wg.Done()
			<-start
			time.Sleep(pruneDelay)
			_, pruneErr = postgres.NewGenerationRetentionStore(l.store).PruneSupersededGenerations(ctx, prunePolicy(1_000_000))
		}()
		close(start)
		wg.Wait()
		cancel()
		if pruneErr != nil {
			t.Fatalf("race %d: prune error %v", i, pruneErr)
		}
		x0Present := l.queryInt(t, `SELECT count(*) FROM scope_generations WHERE generation_id = $1`, x0) == 1
		outcome := ""
		switch reason, retry := linksfreshnessstore.RetryReasonOf(linkErr); {
		case retry && reason == linksfreshnessstore.RetryGenerationLocked:
			outcome = "generation_locked"
			if x0Present {
				// Retention held x0 for another reason and did not prune it.
				l.prune(t, l.ctx)
			}
			if again := mustLink(t, w, l, scope); again.Break != linksfreshnessstore.BreakPriorPruned || again.RebasedFrom != x0 {
				t.Fatalf("race %d: retry after generation_locked = %+v, want a rebase from %s", i, again, x0)
			}
		case linkErr != nil:
			t.Fatalf("race %d: link error %v", i, linkErr)
		case linked.Break == linksfreshnessstore.BreakPriorPruned:
			outcome = "rebase"
			if linked.Kind != linksfreshnessstore.LinkKindRoot || linked.RebasedFrom != x0 || x0Present {
				t.Fatalf("race %d: rebase %+v with x0 present %v", i, linked, x0Present)
			}
		case linked.Kind == linksfreshnessstore.LinkKindIncremental && linked.PriorGenerationID == x0:
			outcome = "link_then_prune"
			if x0Present {
				outcome = "skip"
			}
		default:
			t.Fatalf("race %d: link = %+v, not an allowed ending", i, linked)
		}
		outcomes[outcome]++
		if links, activations, headless := l.probe(t); links+activations+headless != 0 {
			t.Fatalf("race %d (%s): probe = %d/%d/%d, want zeros", i, outcome, links, activations, headless)
		}
		if n := l.headlessDeltas(t); n != 0 {
			t.Fatalf("race %d: %d delta rows have no link row", i, n)
		}
		l.supersede(t, scope, x1, fmt.Sprintf("x%02d-2", i))
	}
	t.Logf("endings over %d races: %v", races, outcomes)
	for l.prune(t, l.ctx).GenerationsPruned > 0 {
	}
	for i := range races {
		if n := l.rowsNaming(t, fmt.Sprintf("x%02d-0", i), fmt.Sprintf("x%02d-1", i)); n != 0 {
			t.Fatalf("race %d: %d ledger rows name a pruned generation", i, n)
		}
	}
	if links, activations, headless := l.probe(t); links+activations+headless != 0 {
		t.Fatalf("probe after the final prunes = %d/%d/%d, want zeros", links, activations, headless)
	}
}

// raceOffset is the start delay of race i's later side.
func raceOffset(i int) time.Duration { return time.Duration((i/2)%8) * 2 * time.Millisecond }

// seedOrphanScope links a chain on scopeID and then deletes its
// ingestion_scopes row (cascading its generations), leaving ledger rows the
// sweeper's orphan purge owns.
func (l *ledgerDB) seedOrphanScope(t *testing.T, w *linksfreshnessstore.LinkWriter, scopeID string, gens []string) {
	t.Helper()
	l.linkChain(t, w, scopeID, gens, time.Now().UTC())
	l.exec(t, `DELETE FROM fact_records WHERE scope_id = $1`, scopeID)
	l.exec(t, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
}

// TestRetentionAndOrphanPurgeDoNotBlock proves retention and the orphan-scope
// purge work on disjoint scopes and never wait on each other: the purge only
// takes a scope with no ingestion_scopes row, and retention holds its scopes'
// rows FOR UPDATE, so a scope cannot be deleted (and so purged) while
// retention prunes it. Both directions are held open, then 20 real races run.
func TestRetentionAndOrphanPurgeDoNotBlock(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	journal := linksfreshnessstore.NewJournalStore(l.store)

	// Purge in flight, retention runs.
	l.linkChain(t, w, "scope-p1", []string{"p1-0", "p1-1"}, fixtureEpoch.Add(-48*time.Hour))
	l.seedOrphanScope(t, w, "orphan-1", []string{"o1-0", "o1-1"})
	purge, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin purge: %v", err)
	}
	for _, table := range []string{
		"changed_since_link_deltas", "changed_since_link_bucket_counts", "changed_since_links",
		"changed_since_key_state", "changed_since_activations", "changed_since_scope_cursor",
	} {
		if _, err := purge.ExecContext(l.ctx, `DELETE FROM `+table+` WHERE scope_id = 'orphan-1'`); err != nil {
			t.Fatalf("purge %s: %v", table, err)
		}
	}
	ctx, cancel := context.WithTimeout(l.ctx, noWait)
	if result := l.prune(t, ctx); result.GenerationsPruned != 1 {
		t.Fatalf("pruned %d with a purge in flight, want 1", result.GenerationsPruned)
	}
	cancel()
	if err := purge.Commit(); err != nil {
		t.Fatalf("commit purge: %v", err)
	}

	// Retention in flight, purge runs; the retained scope cannot be deleted.
	l.linkChain(t, w, "scope-p2", []string{"p2-0", "p2-1"}, fixtureEpoch.Add(-48*time.Hour))
	l.seedOrphanScope(t, w, "orphan-2", []string{"o2-0", "o2-1"})
	retention, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin retention: %v", err)
	}
	defer func() { _ = retention.Rollback() }()
	if _, err := retention.ExecContext(l.ctx, `SELECT 1 FROM ingestion_scopes WHERE scope_id = 'scope-p2' FOR UPDATE`); err != nil {
		t.Fatalf("lock scope-p2: %v", err)
	}
	if _, err := linksfreshnessstore.DeletePrunedGenerationRows(l.ctx, postgres.SQLTx{Tx: retention}, []string{"scope-p2"}, []string{"p2-0"}); err != nil {
		t.Fatalf("DeletePrunedGenerationRows: %v", err)
	}
	ctx, cancel = context.WithTimeout(l.ctx, noWait)
	purged, err := journal.DeleteOrphanScope(ctx, "orphan-2")
	cancel()
	if err != nil || !purged {
		t.Fatalf("DeleteOrphanScope with retention in flight = %v, %v; want purged", purged, err)
	}
	conn, err := l.raw.Conn(l.ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	_, _ = conn.ExecContext(l.ctx, `SET lock_timeout = '300ms'`)
	_, err = conn.ExecContext(l.ctx, `DELETE FROM ingestion_scopes WHERE scope_id = 'scope-p2'`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("deleting a scope retention holds = %v, want lock_not_available", err)
	}
	_ = conn.Close()
	if err := retention.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("rollback retention: %v", err)
	}

	// Real races.
	for i := range 20 {
		scope, orphan := fmt.Sprintf("scope-q%02d", i), fmt.Sprintf("orphan-q%02d", i)
		g, o := fmt.Sprintf("q%02d", i), fmt.Sprintf("oq%02d", i)
		l.linkChain(t, w, scope, []string{g + "-0", g + "-1"}, fixtureEpoch.Add(-48*time.Hour))
		l.seedOrphanScope(t, w, orphan, []string{o + "-0", o + "-1"})
		ctx, cancel := context.WithTimeout(l.ctx, noWait)
		var wg sync.WaitGroup
		var purgeErr, pruneErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, purgeErr = journal.DeleteOrphanScope(ctx, orphan)
		}()
		go func() {
			defer wg.Done()
			_, pruneErr = postgres.NewGenerationRetentionStore(l.store).PruneSupersededGenerations(ctx, prunePolicy(1_000_000))
		}()
		wg.Wait()
		cancel()
		if purgeErr != nil || pruneErr != nil {
			t.Fatalf("race %d: purge error %v, prune error %v", i, purgeErr, pruneErr)
		}
		if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links WHERE scope_id = $1`, orphan); n != 0 {
			t.Fatalf("race %d: %d links of the purged scope remain", i, n)
		}
		if n := l.rowsNaming(t, g+"-0"); n != 0 {
			t.Fatalf("race %d: %d ledger rows name the pruned %s-0", i, n, g)
		}
	}
}

// TestSplitLedgerDeleteLeaksHeadlessDeltas is the RED of P2 (arbiter ruling
// arb-7127-3d): the ruling-2.8 deletes issued as separate statements, with a
// late link committing between the delta delete and the link delete, leave
// delta rows with no link row, which no rule reading pairs from
// changed_since_links can find again. The shipped statement is one WITH;
// TestRetentionDoesNotWaitOnALinkInFlight proves it leaves none.
func TestSplitLedgerDeleteLeaksHeadlessDeltas(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedRootedScope(t, w, "scope-s", "s0", "s1")

	split, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin split: %v", err)
	}
	defer func() { _ = split.Rollback() }()
	deleteDeltas := `DELETE FROM changed_since_link_deltas AS d USING changed_since_links AS l
WHERE l.scope_id = 'scope-s' AND (l.generation_id = 's0' OR l.prior_generation_id = 's0')
  AND d.scope_id = l.scope_id AND d.generation_id = l.generation_id AND d.prior_generation_id = l.prior_generation_id`
	if _, err := split.ExecContext(l.ctx, deleteDeltas); err != nil {
		t.Fatalf("split delta delete: %v", err)
	}
	// The real writer links (s0 -> s1) and commits between the statements.
	if result, err := w.LinkNext(l.ctx, "scope-s"); err != nil || result.PriorGenerationID != "s0" {
		t.Fatalf("late link = %+v, %v", result, err)
	}
	for _, s := range []string{
		`DELETE FROM changed_since_link_bucket_counts AS b USING changed_since_links AS l
WHERE l.scope_id = 'scope-s' AND (l.generation_id = 's0' OR l.prior_generation_id = 's0')
  AND b.scope_id = l.scope_id AND b.generation_id = l.generation_id AND b.prior_generation_id = l.prior_generation_id`,
		`DELETE FROM changed_since_links WHERE scope_id = 'scope-s' AND (generation_id = 's0' OR prior_generation_id = 's0')`,
	} {
		if _, err := split.ExecContext(l.ctx, s); err != nil {
			t.Fatalf("split step %q: %v", firstLine(s), err)
		}
	}
	if err := split.Commit(); err != nil {
		t.Fatalf("commit split: %v", err)
	}
	if n := l.headlessDeltas(t); n == 0 {
		t.Fatal("split deletes left no headless delta rows; the RED cannot see the leak")
	} else {
		t.Logf("split deletes leaked %d delta rows with no link row (expected RED)", n)
	}
}
