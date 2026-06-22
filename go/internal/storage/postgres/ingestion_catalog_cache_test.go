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
// query from the outer (non-transactional) connection and records how many
// times that query executed. It lets the cache regression tests assert that N
// commits cause O(1) catalog loads instead of O(N).
type countingCatalogDB struct {
	mu             sync.Mutex
	txs            []*fakeTx
	beginCalls     int
	catalogQueries int
	// catalogPayloads is the set of repository payloads returned by every
	// catalog load. Tests mutate it between commits to model onboarding.
	catalogPayloads [][]byte
}

func (f *countingCatalogDB) Begin(context.Context) (Transaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beginCalls++
	if len(f.txs) == 0 {
		return &fakeTx{}, nil
	}
	tx := f.txs[0]
	f.txs = f.txs[1:]
	return tx, nil
}

func (f *countingCatalogDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (f *countingCatalogDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(query, "fact_kind = 'repository'") {
		f.catalogQueries++
		rows := make([][]any, 0, len(f.catalogPayloads))
		for _, payload := range f.catalogPayloads {
			rows = append(rows, []any{payload})
		}
		return &queueFakeRows{rows: rows}, nil
	}
	return &queueFakeRows{}, nil
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
