package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// catalogBenchRepoCount models the live platform-qa fleet scale that triggered issue
// #3481 (≈907 repos). The catalog SELECT is unbounded, so per-commit reloads
// scan all of these rows on every commit.
const catalogBenchRepoCount = 1_000

// catalogBenchCommits is the number of known-repo commits each benchmark drives.
// The whole point of the fix is that catalog loads stay flat (1) instead of
// scaling with this count.
const catalogBenchCommits = 200

// benchCatalogPayloads builds a repository catalog of the given size.
func benchCatalogPayloads(count int) [][]byte {
	payloads := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		payloads = append(payloads, []byte(fmt.Sprintf(`{"graph_id":"repo-%d"}`, i)))
	}
	return payloads
}

// BenchmarkIngestionStoreCatalogLoadsPerCommit reports catalog_loads_per_commit
// for the shared-cache hot path. It is the durable before/after evidence for
// issue #3481: the pre-fix path loaded the full repository catalog on every
// commit (loads/op == 1.0, each scanning all repos), while the cached path
// amortizes loads to ~0 per commit once warm.
//
// Run:
//
//	go test ./internal/storage/postgres -run x \
//	  -bench BenchmarkIngestionStoreCatalogLoadsPerCommit -benchmem
func BenchmarkIngestionStoreCatalogLoadsPerCommit(b *testing.B) {
	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)

	b.Run("cached", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			db := &countingCatalogDB{catalogPayloads: benchCatalogPayloads(catalogBenchRepoCount)}
			store := NewIngestionStore(db)
			store.Now = func() time.Time { return now }
			store.SkipRelationshipBackfill = true
			runKnownRepoCommits(b, store, catalogBenchCommits, now)
			reportCatalogLoads(b, db.catalogQueries, catalogBenchCommits)
		}
	})

	b.Run("per_commit_baseline", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			db := &countingCatalogDB{catalogPayloads: benchCatalogPayloads(catalogBenchRepoCount)}
			// A store with a nil cache reproduces the pre-#3481 behavior: every
			// commit reloads the full catalog (O(N) per commit).
			store := NewIngestionStore(db)
			store.catalogCache = nil
			store.Now = func() time.Time { return now }
			store.SkipRelationshipBackfill = true
			runKnownRepoCommits(b, store, catalogBenchCommits, now)
			reportCatalogLoads(b, db.catalogQueries, catalogBenchCommits)
		}
	})
}

// runKnownRepoCommits commits the requested number of generations for one
// already-known repository through the store.
func runKnownRepoCommits(b *testing.B, store IngestionStore, commits int, now time.Time) {
	b.Helper()
	for i := 0; i < commits; i++ {
		generationID := fmt.Sprintf("gen-%d", i)
		err := store.CommitScopeGeneration(
			context.Background(),
			catalogTestScope("scope-0", "repo-0"),
			catalogTestGeneration("scope-0", generationID, now),
			testFactChannel([]facts.Envelope{
				catalogRepositoryFact("scope-0", generationID, "repo-0", now.Add(-time.Minute)),
			}),
		)
		if err != nil {
			b.Fatalf("commit %d: CommitScopeGeneration() error = %v", i, err)
		}
	}
}

// reportCatalogLoads records loads-per-commit as a custom benchmark metric.
func reportCatalogLoads(b *testing.B, loads, commits int) {
	b.Helper()
	b.ReportMetric(float64(loads)/float64(commits), "catalog_loads/commit")
}
