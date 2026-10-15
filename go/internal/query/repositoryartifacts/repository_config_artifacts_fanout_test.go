// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// fanoutLatencyStore serves a small Terraform set per repository and parks
// 50ms in every list, so a serial cross-repo walk over eight repositories
// takes ~400ms while an eight-wide fan-out takes one wave. It records the
// peak concurrent lists to prove the bound both ways (parallel, not
// unbounded). Every method is safe for concurrent use.
type fanoutLatencyStore struct {
	querycontract.ContentStore
	files   map[string]string
	current atomic.Int64
	peak    atomic.Int64
}

func newFanoutLatencyStore() *fanoutLatencyStore {
	return &fanoutLatencyStore{files: moduleFiles(5)}
}

func (s *fanoutLatencyStore) track() func() {
	inFlight := s.current.Add(1)
	for {
		old := s.peak.Load()
		if inFlight <= old || s.peak.CompareAndSwap(old, inFlight) {
			break
		}
	}
	return func() { s.current.Add(-1) }
}

func (s *fanoutLatencyStore) ListRepoFiles(_ context.Context, repoID string, _ int) ([]querycontract.FileContent, error) {
	release := s.track()
	defer release()
	time.Sleep(50 * time.Millisecond)
	out := make([]querycontract.FileContent, 0, len(s.files))
	for path := range s.files {
		out = append(out, querycontract.FileContent{RepoID: repoID, RelativePath: path})
	}
	return out, nil
}

func (s *fanoutLatencyStore) GetFileContents(_ context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	out := make([]querycontract.FileContent, 0, len(paths))
	for _, path := range paths {
		out = append(out, querycontract.FileContent{RepoID: repoID, RelativePath: path, Content: s.files[path]})
	}
	return out, nil
}

func (s *fanoutLatencyStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, nil
}

func (s *fanoutLatencyStore) NonBlankRepoFilePaths(_ context.Context, _ string, paths []string) ([]string, error) {
	return paths, nil
}

// TestConfigArtifactLoadFansOutAcrossRepositories proves the cross-repo walk
// overlaps repository loads: eight repositories at 50ms of list latency each
// must finish well under the ~400ms a serial walk needs. It fails on the
// serial loader and passes on the bounded fan-out, and it pins the bound
// from both sides (parallel, never more than eight lists at once).
func TestConfigArtifactLoadFansOutAcrossRepositories(t *testing.T) {
	t.Parallel()

	store := newFanoutLatencyStore()
	sources := make([]repositoryArtifactSource, 0, 8)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("repo-%d", i)
		sources = append(sources, repositoryArtifactSource{RepoID: id, RepoName: "name-" + id})
	}
	var stats ConfigArtifactReadStats
	started := time.Now()
	got, err := loadRepositoryConfigArtifactsForSources(context.Background(), store, sources, &stats)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	if rows := querycontract.MapSliceValue(got, "config_paths"); len(rows) == 0 {
		t.Fatal("fixture produced no rows; the timing proves nothing")
	}
	if elapsed >= 300*time.Millisecond {
		t.Fatalf("eight repositories at 50ms list latency took %v, want under 300ms (serial takes ~400ms)", elapsed)
	}
	if peak := store.peak.Load(); peak < 2 {
		t.Fatalf("peak concurrent lists = %d, want at least 2 (no overlap observed)", peak)
	} else if peak > 8 {
		t.Fatalf("peak concurrent lists = %d, want at most 8 (unbounded fan-out)", peak)
	}
}

// jitterStore wraps a budgetStore with a small random sleep in every read, so
// a determinism test exercises completion orders a serial walk never sees.
// The generator is mutex-guarded: the fan-out calls the store concurrently.
type jitterStore struct {
	*budgetStore
	mu  sync.Mutex
	rng *rand.Rand
}

func (s *jitterStore) sleep() {
	s.mu.Lock()
	nap := s.rng.Intn(2000)
	s.mu.Unlock()
	time.Sleep(time.Duration(nap) * time.Microsecond)
}

func (s *jitterStore) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.sleep()
	return s.budgetStore.ListRepoFiles(ctx, repoID, limit)
}

func (s *jitterStore) GetFileContents(ctx context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	s.sleep()
	return s.budgetStore.GetFileContents(ctx, repoID, paths)
}

func (s *jitterStore) NonBlankRepoFilePaths(ctx context.Context, repoID string, paths []string) ([]string, error) {
	s.sleep()
	return s.budgetStore.NonBlankRepoFilePaths(ctx, repoID, paths)
}

// TestConfigArtifactFanOutStaysDeterministicUnderJitter proves the fan-out
// keeps byte-identical bodies and identical counts across completion orders:
// five jittered runs of the budget-bite mix (which exercises both the
// optimistic adopt and the sequential repair) must agree exactly.
func TestConfigArtifactFanOutStaysDeterministicUnderJitter(t *testing.T) {
	t.Parallel()

	loader := productionConfigArtifactLoader
	loader.budget = 100
	ids := []string{"repo-d", "repo-b", "repo-a", "repo-c"}
	var firstBody string
	var firstStats ConfigArtifactReadStats
	for round := int64(0); round < 5; round++ {
		store, _ := budgetFixture()
		jittered := &jitterStore{budgetStore: store, rng: rand.New(rand.NewSource(7126 + round))}
		overview, stats, err := loader.loadDeploymentArtifactOverviewWithStats(
			context.Background(), relatedGraph{ids: ids}, jittered, "repo-anchor", "anchor",
			[]querycontract.FileContent{}, nil, nil,
		)
		if err != nil {
			t.Fatalf("round %d: load error = %v", round, err)
		}
		body, err := json.Marshal(overview)
		if err != nil {
			t.Fatalf("round %d: marshal: %v", round, err)
		}
		counts := statsCounts(stats)
		if round == 0 {
			firstBody, firstStats = string(body), counts
			continue
		}
		if string(body) != firstBody {
			t.Fatalf("round %d: body differs under jitter", round)
		}
		if counts != firstStats {
			t.Fatalf("round %d: counts %+v differ from %+v under jitter", round, counts, firstStats)
		}
	}
}

// oracleCounts is the contract-guaranteed part of the stats: every per-repo
// aggregated count. It excludes the durations and the in-flight peak
// (timing-dependent) and Statements with HydratedBytes, which meter physical
// store I/O: the fan-out issues optimistic reads and sequential repair
// re-reads on top of the adopted per-repo outcomes, so those two meters
// honestly exceed the serial walk exactly when the budget bites. They match
// the serial walk whenever no repository is truncated, which is the
// production case the budget sizing holds.
func oracleCounts(stats ConfigArtifactReadStats) ConfigArtifactReadStats {
	stats = statsCounts(stats)
	stats.Statements, stats.HydratedBytes = 0, 0
	return stats
}

// TestConfigArtifactFanOutMatchesSequentialDigests is the row-equality oracle
// against the serial loader: SHA-256 of the marshalled config_paths and of
// the whole overview body on the budget-bite reference mix, captured from the
// serial implementation before the fan-out landed. Any row or order change
// moves the digests.
func TestConfigArtifactFanOutMatchesSequentialDigests(t *testing.T) {
	t.Parallel()

	loader := productionConfigArtifactLoader
	loader.budget = 100
	store, _ := budgetFixture()
	overview, stats, err := loader.loadDeploymentArtifactOverviewWithStats(
		context.Background(), relatedGraph{ids: []string{"repo-d", "repo-b", "repo-a", "repo-c"}},
		store, "repo-anchor", "anchor", []querycontract.FileContent{}, nil, nil,
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	rowsBody, err := json.Marshal(configPathRowsOf(overview))
	if err != nil {
		t.Fatalf("marshal rows: %v", err)
	}
	wholeBody, err := json.Marshal(overview)
	if err != nil {
		t.Fatalf("marshal overview: %v", err)
	}
	countsBody, err := json.Marshal(oracleCounts(stats))
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}
	digest := func(body []byte) string {
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	}
	if got, want := digest(rowsBody), "6e08c26ebb0f995cce9103895e1ccbf0d6a2b2887589229008f679d3fe358443"; got != want {
		t.Fatalf("rows=%s whole=%s counts=%s", got, digest(wholeBody), digest(countsBody))
	}
	if got, want := digest(wholeBody), "7d1e411a25f42942de7d516061777e3547e8962b3768d9ff41921a0b7d38fb4f"; got != want {
		t.Fatalf("whole=%s rows=%s counts=%s", got, digest(rowsBody), digest(countsBody))
	}
	if got, want := digest(countsBody), "5cb897f6189fd330cd059239d902e29ee7a6b215df69139c0762109316f5bfe6"; got != want {
		t.Fatalf("counts=%s rows=%s whole=%s", got, digest(rowsBody), digest(wholeBody))
	}
}
