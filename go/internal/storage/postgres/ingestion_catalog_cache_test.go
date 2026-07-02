// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// countingCatalogDB is a transactional fake that serves the repository catalog
// query and records how many times it executed. The catalog load now runs on
// the open ingestion transaction's connection (issue #3481 P1 fix: the cold
// cache must not acquire a second pool connection while the commit tx is open),
// so the count is incremented wherever the catalog query lands — tx or outer.
//
// outerCatalogWhileTxOpen flags any catalog read served by the outer
// (non-transactional) connection while a transaction was open. It models a
// single-connection pool (ESHU_POSTGRES_MAX_OPEN_CONNS=1): asking the pool for a
// second connection while the commit tx holds the only one would deadlock. The
// P1 regression test asserts this flag stays false.
type countingCatalogDB struct {
	mu             sync.Mutex
	beginCalls     int
	openTx         int
	catalogQueries int
	// outerCatalogWhileTxOpen is set if the outer pool served a catalog read
	// while a transaction was open. It must never happen after the P1 fix.
	outerCatalogWhileTxOpen bool
	// catalogPayloads is the set of repository payloads returned by every
	// catalog load. Tests mutate it between commits to model onboarding.
	catalogPayloads [][]byte
}

func (f *countingCatalogDB) Begin(context.Context) (Transaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beginCalls++
	f.openTx++
	return &catalogTx{db: f}, nil
}

func (f *countingCatalogDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

// catalogRows builds the configured catalog rows and increments the shared load
// counter. The caller holds f.mu.
func (f *countingCatalogDB) catalogRows() Rows {
	f.catalogQueries++
	rows := make([][]any, 0, len(f.catalogPayloads))
	for _, payload := range f.catalogPayloads {
		rows = append(rows, []any{payload})
	}
	return &queueFakeRows{rows: rows}
}

func (f *countingCatalogDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(query, "fact_kind = 'repository'") {
		if f.openTx > 0 {
			// A catalog read on the outer pool while a tx is open is exactly the
			// second-connection acquisition the P1 fix forbids.
			f.outerCatalogWhileTxOpen = true
		}
		return f.catalogRows(), nil
	}
	return &queueFakeRows{}, nil
}

// catalogTx serves the catalog read on the open transaction's own connection and
// records commit/rollback so the harness can track open transactions.
type catalogTx struct {
	db        *countingCatalogDB
	committed bool
}

func (t *catalogTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (t *catalogTx) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if strings.Contains(query, "fact_kind = 'repository'") {
		return t.db.catalogRows(), nil
	}
	if strings.Contains(query, "INSERT INTO fact_records") && strings.Contains(query, "RETURNING fact_id") {
		// Default: every fact_id in the batch is accepted (no fencing
		// conflict). Without this, afterBatch would always see an empty
		// filtered batch and this harness's repository-onboarding detection
		// (which reads FactKind=="repository" envelopes from afterBatch) would
		// never fire, breaking the catalog-reload assertions this file makes.
		return &queueFakeRows{rows: fakeAcceptedFactIDRows(args)}, nil
	}
	return &queueFakeRows{}, nil
}

func (t *catalogTx) Commit() error {
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if !t.committed {
		t.committed = true
		t.db.openTx--
	}
	return nil
}

func (t *catalogTx) Rollback() error {
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if !t.committed {
		t.committed = true
		t.db.openTx--
	}
	return nil
}

func catalogTestScope(scopeID, repoID string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  repoID,
	}
}

func catalogTestGeneration(scopeID, generationID string, now time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func catalogRepositoryFact(scopeID, generationID, repoID string, observedAt time.Time) facts.Envelope {
	return facts.Envelope{
		FactID:        "fact-" + repoID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      "repository",
		StableFactKey: "repository:" + repoID,
		ObservedAt:    observedAt,
		Payload:       map[string]any{"graph_id": repoID},
		SourceRef: facts.Ref{
			SourceSystem: "git",
			FactKey:      "fact-key-" + repoID,
		},
	}
}

// TestIngestionStoreReusesRepositoryCatalogAcrossCommits pins the #3481 hot-path
// regression: committing N already-known scope generations must not reload the
// global repository catalog per commit. With the shared cache the catalog loads
// once (O(1)); the pre-fix per-commit load was O(N).
func TestIngestionStoreReusesRepositoryCatalogAcrossCommits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true

	const commits = 5
	for i := 0; i < commits; i++ {
		generationID := "gen-" + string(rune('a'+i))
		envelopes := []facts.Envelope{
			catalogRepositoryFact("scope-known", generationID, "repo-known", now.Add(-time.Minute)),
		}
		err := store.CommitScopeGeneration(
			context.Background(),
			catalogTestScope("scope-known", "repo-known"),
			catalogTestGeneration("scope-known", generationID, now),
			testFactChannel(envelopes),
		)
		if err != nil {
			t.Fatalf("commit %d: CommitScopeGeneration() error = %v, want nil", i, err)
		}
	}

	if got := db.catalogQueries; got != 1 {
		t.Fatalf("repository catalog loads = %d across %d commits, want 1 (O(1), not O(N))", got, commits)
	}
}

// TestIngestionStoreReloadsRepositoryCatalogAfterNewRepository proves the cache
// stays accurate: when a commit introduces a repository the cached catalog has
// not seen, the cache invalidates so the next commit observes the new repo.
func TestIngestionStoreReloadsRepositoryCatalogAfterNewRepository(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true

	// Commit 1: a known repo. Loads the catalog once and caches it.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-1", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFact("scope-known", "gen-1", "repo-known", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 1: CommitScopeGeneration() error = %v, want nil", err)
	}

	// Commit 2: a brand-new repo not in the cached catalog. This must
	// invalidate the cache so the new identity becomes visible.
	db.mu.Lock()
	db.catalogPayloads = append(db.catalogPayloads, []byte(`{"graph_id":"repo-new"}`))
	db.mu.Unlock()
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-new", "repo-new"),
		catalogTestGeneration("scope-new", "gen-2", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFact("scope-new", "gen-2", "repo-new", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 2: CommitScopeGeneration() error = %v, want nil", err)
	}

	// Commit 3: another known repo. The cache should have reloaded after the
	// new repo in commit 2, so this commit reuses the refreshed cache.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-3", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFact("scope-known", "gen-3", "repo-known", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 3: CommitScopeGeneration() error = %v, want nil", err)
	}

	// Exactly two loads: the initial load, then one reload triggered by the
	// new repo in commit 2. Commit 3 reuses the refreshed cache.
	if got := db.catalogQueries; got != 2 {
		t.Fatalf("repository catalog loads = %d, want 2 (initial + one reload after onboarding)", got)
	}
}

// TestIngestionStoreSharedCatalogCacheIsConcurrencySafe proves the shared cache
// is safe under concurrent commit workers (run under -race) and still bounds the
// global catalog reloads. Many goroutines commit known-repo generations through
// one shared store; the catalog must load a small bounded number of times, never
// once per commit, and never race.
func TestIngestionStoreSharedCatalogCacheIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		go func(worker int) {
			defer wg.Done()
			generationID := "gen-concurrent-" + string(rune('a'+worker))
			err := store.CommitScopeGeneration(
				context.Background(),
				catalogTestScope("scope-known", "repo-known"),
				catalogTestGeneration("scope-known", generationID, now),
				testFactChannel([]facts.Envelope{
					catalogRepositoryFact("scope-known", generationID, "repo-known", now.Add(-time.Minute)),
				}),
			)
			if err != nil {
				errs <- err
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent CommitScopeGeneration() error = %v, want nil", err)
	}

	// All workers commit the same already-known repo, so the catalog must load a
	// small bounded number of times (at most one per racing cold-cache reader on
	// the very first commits), never once per commit. A value at or below the
	// worker count that is strictly less than the commit count proves the
	// O(1)-amortized contract while tolerating a cold-start race window.
	if got := db.catalogQueries; got < 1 || got >= workers {
		t.Fatalf("repository catalog loads = %d across %d concurrent commits, want bounded (>=1, <%d)", got, workers, workers)
	}
}

// TestIngestionStoreLoadsCatalogOnOpenTransaction is the #3521 P1 regression
// test: the cold-cache catalog load must run on the open ingestion transaction's
// connection, not by acquiring a second pool connection while the tx is open.
// With ESHU_POSTGRES_MAX_OPEN_CONNS=1 (or a saturated pool) a second acquisition
// would block forever, deadlocking the committer. The harness flags any catalog
// read served by the outer pool while a transaction is open.
func TestIngestionStoreLoadsCatalogOnOpenTransaction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true

	// Cold cache: this commit must load the catalog, and it must do so on the
	// transaction connection.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-1", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFact("scope-known", "gen-1", "repo-known", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	if db.catalogQueries != 1 {
		t.Fatalf("catalog loads = %d, want 1 (cold cache loads once)", db.catalogQueries)
	}
	if db.outerCatalogWhileTxOpen {
		t.Fatal("catalog was read from the outer pool while the ingestion tx was open: " +
			"a second connection acquisition can deadlock under a single-connection pool")
	}
	if db.openTx != 0 {
		t.Fatalf("open transactions after commit = %d, want 0", db.openTx)
	}
}

// catalogRepositoryFactWithSlug builds a repository fact carrying a repo_slug
// alias so alias-drift tests can mutate identity across commits.
func catalogRepositoryFactWithSlug(scopeID, generationID, repoID, slug string, observedAt time.Time) facts.Envelope {
	fact := catalogRepositoryFact(scopeID, generationID, repoID, observedAt)
	fact.Payload = map[string]any{"graph_id": repoID, "repo_slug": slug}
	return fact
}

// TestIngestionStoreReloadsCatalogWhenKnownRepoAliasDrifts is the #3521 P2
// regression test: invalidation must trigger when an already-known repo's
// identity aliases (slug/name) change, not only when a new repo id appears.
// DiscoverEvidence matches via CatalogEntry.Aliases, so a stale alias silently
// drops cross-repo evidence for the renamed slug until an unrelated new repo
// evicts the cache.
func TestIngestionStoreReloadsCatalogWhenKnownRepoAliasDrifts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-known","repo_slug":"old-slug"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true

	// Commit 1: caches the catalog with alias "old-slug".
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-1", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFactWithSlug("scope-known", "gen-1", "repo-known", "old-slug", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 1: CommitScopeGeneration() error = %v, want nil", err)
	}

	// The committed generation renames the slug. The durable catalog reflects
	// the new slug for the next load.
	db.mu.Lock()
	db.catalogPayloads = [][]byte{[]byte(`{"graph_id":"repo-known","repo_slug":"new-slug"}`)}
	db.mu.Unlock()

	// Commit 2: same repo id, drifted slug. This must invalidate so a later
	// commit observes the new alias.
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-2", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFactWithSlug("scope-known", "gen-2", "repo-known", "new-slug", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 2: CommitScopeGeneration() error = %v, want nil", err)
	}

	// Commit 3: reuses the refreshed cache (no further drift).
	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-known", "repo-known"),
		catalogTestGeneration("scope-known", "gen-3", now),
		testFactChannel([]facts.Envelope{
			catalogRepositoryFactWithSlug("scope-known", "gen-3", "repo-known", "new-slug", now.Add(-time.Minute)),
		}),
	); err != nil {
		t.Fatalf("commit 3: CommitScopeGeneration() error = %v, want nil", err)
	}

	// Two loads: the initial cache fill, then one reload after the slug drift in
	// commit 2. If alias drift did not invalidate, this stays at 1 and the test
	// fails (the pre-fix behavior).
	if got := db.catalogQueries; got != 2 {
		t.Fatalf("repository catalog loads = %d, want 2 (initial + one reload after alias drift)", got)
	}
}
