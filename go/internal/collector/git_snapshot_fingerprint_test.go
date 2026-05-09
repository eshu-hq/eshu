package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestBuildCollectedGenerationDerivesStableFreshnessHintForEquivalentSnapshots(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	sourceRunID := "source-run-123"
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshotA := testCollectorSnapshot(repoPath, "def handler():\n    return 1\n", "digest-1")
	snapshotB := testCollectorSnapshot(repoPath, "def handler():\n    return 1\n", "digest-1")

	collectedA := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotA, false)
	collectedB := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotB, false)

	if got, want := collectedA.Generation.FreshnessHint, collectedB.Generation.FreshnessHint; got != want {
		t.Fatalf("FreshnessHint mismatch for equivalent snapshots: got %q, want %q", got, want)
	}
	if got, want := collectedA.Generation.GenerationID, collectedB.Generation.GenerationID; got != want {
		t.Fatalf("GenerationID mismatch for equivalent snapshots: got %q, want %q", got, want)
	}
	if got, want := collectedA.FactCount, collectedB.FactCount; got != want {
		t.Fatalf("Fact count mismatch for equivalent snapshots: got %d, want %d", got, want)
	}
}

func TestBuildCollectedGenerationChangesFreshnessHintForMateriallyDifferentSnapshots(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	sourceRunID := "source-run-123"
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshotA := testCollectorSnapshot(repoPath, "def handler():\n    return 1\n", "digest-1")
	snapshotB := testCollectorSnapshot(repoPath, "def handler():\n    return 2\n", "digest-2")

	collectedA := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotA, false)
	collectedB := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotB, false)

	if got, want := collectedA.Generation.FreshnessHint, collectedB.Generation.FreshnessHint; got == want {
		t.Fatalf("FreshnessHint = %q for materially different snapshots, want different values", got)
	}
}

func TestBuildCollectedGenerationChangesFreshnessHintWhenImportsMapChanges(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	sourceRunID := "source-run-123"
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshotA := testCollectorSnapshot(repoPath, "def handler():\n    return 1\n", "digest-1")
	snapshotB := testCollectorSnapshot(repoPath, "def handler():\n    return 1\n", "digest-1")
	snapshotA.ImportsMap = map[string][]string{
		"Helper": {repoPath + "/helpers.py"},
	}
	snapshotB.ImportsMap = map[string][]string{
		"Handler": {repoPath + "/handlers.py"},
	}

	collectedA := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotA, false)
	collectedB := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotB, false)

	if got, want := collectedA.Generation.FreshnessHint, collectedB.Generation.FreshnessHint; got == want {
		t.Fatalf("FreshnessHint = %q for changed imports_map, want different values", got)
	}
}

func TestGitSourceNextKeepsGenerationAndFactsStableAcrossSnapshotChanges(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	repoPath := t.TempDir()
	firstSnapshot := testCollectorSnapshot(repoPath, "def handler():\n    return 1\n", "digest-1")
	secondSnapshot := testCollectorSnapshot(repoPath, "def handler():\n    return 2\n", "digest-2")

	firstSource := newTestCollectorGitSource(observedAt, repoPath, firstSnapshot)
	secondSource := newTestCollectorGitSource(observedAt, repoPath, secondSnapshot)

	firstCollected, ok, err := firstSource.Next(context.Background())
	if err != nil {
		t.Fatalf("first Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("first Next() ok = false, want true")
	}

	secondCollected, ok, err := secondSource.Next(context.Background())
	if err != nil {
		t.Fatalf("second Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("second Next() ok = false, want true")
	}

	if got, want := firstCollected.Generation.GenerationID, secondCollected.Generation.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := firstCollected.Generation.FreshnessHint, secondCollected.Generation.FreshnessHint; got == want {
		t.Fatalf("FreshnessHint = %q for changed snapshot, want different values", got)
	}

	firstFacts := drainFactChannel(firstCollected.Facts)
	secondFacts := drainFactChannel(secondCollected.Facts)

	if got, want := len(firstFacts), len(secondFacts); got != want {
		t.Fatalf("Fact count = %d, want %d", got, want)
	}
	if got, want := firstFacts[0].FactID, secondFacts[0].FactID; got != want {
		t.Fatalf("repository fact ID = %q, want %q", got, want)
	}
	if got, want := firstFacts[2].FactID, secondFacts[2].FactID; got != want {
		t.Fatalf("content fact ID = %q, want %q", got, want)
	}
	if got, want := firstFacts[2].Payload["content_body"], "def handler():\n    return 1\n"; got != want {
		t.Fatalf("first content body = %#v, want %#v", got, want)
	}
	if got, want := secondFacts[2].Payload["content_body"], "def handler():\n    return 2\n"; got != want {
		t.Fatalf("second content body = %#v, want %#v", got, want)
	}
}

func TestBuildCollectedGenerationDerivesStableFreshnessHintForEquivalentMetaSnapshots(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "app.py"), "def handler():\n    return 1\n")
	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	sourceRunID := "source-run-123"
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshotA := testCollectorSnapshotWithMetas(repoPath, "def handler():\n    return 1\n", "digest-1")
	snapshotB := testCollectorSnapshotWithMetas(repoPath, "def handler():\n    return 1\n", "digest-1")

	collectedA := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotA, false)
	collectedB := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotB, false)

	if got, want := collectedA.Generation.FreshnessHint, collectedB.Generation.FreshnessHint; got != want {
		t.Fatalf("FreshnessHint mismatch for equivalent meta snapshots: got %q, want %q", got, want)
	}
}

func TestBuildCollectedGenerationChangesFreshnessHintForDifferentMetaSnapshots(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "app.py"), "def handler():\n    return 1\n")
	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	sourceRunID := "source-run-123"
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshotA := testCollectorSnapshotWithMetas(repoPath, "def handler():\n    return 1\n", "digest-1")
	snapshotB := testCollectorSnapshotWithMetas(repoPath, "def handler():\n    return 2\n", "digest-2")

	collectedA := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotA, false)
	collectedB := buildStreamingGeneration(repoPath, repo, sourceRunID, observedAt, snapshotB, false)

	if got, want := collectedA.Generation.FreshnessHint, collectedB.Generation.FreshnessHint; got == want {
		t.Fatalf("FreshnessHint = %q for materially different meta snapshots, want different values", got)
	}
}

func TestStreamFactsReReadsBodyFromDisk(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := "def handler():\n    return 1\n"
	writeCollectorTestFile(t, filepath.Join(repoPath, "app.py"), body)

	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshotWithMetas(repoPath, body, "digest-1")

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	allFacts := drainFactChannel(collected.Facts)

	// Facts: 1 repo + 1 file + 1 content + 1 entity + 7 followups = 11
	if got, want := len(allFacts), 11; got != want {
		t.Fatalf("fact count = %d, want %d", got, want)
	}

	// Content fact is at index 2 (repo=0, file=1, content=2)
	contentFact := allFacts[2]
	if contentFact.FactKind != "content" {
		t.Fatalf("allFacts[2].FactKind = %q, want %q", contentFact.FactKind, "content")
	}
	gotBody, _ := contentFact.Payload["content_body"].(string)
	if gotBody != body {
		t.Fatalf("content_body = %q, want %q", gotBody, body)
	}
	gotDigest, _ := contentFact.Payload["content_digest"].(string)
	if gotDigest != "digest-1" {
		t.Fatalf("content_digest = %q, want %q", gotDigest, "digest-1")
	}
}

func TestBuildStreamingGenerationTagsGitFactsAsObserved(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "def handler():\n    return 1\n", "digest-1")

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	allFacts := drainFactChannel(collected.Facts)

	if len(allFacts) == 0 {
		t.Fatal("fact count = 0, want git facts")
	}
	for _, fact := range allFacts {
		if got, want := fact.CollectorKind, "git"; got != want {
			t.Fatalf("fact %q CollectorKind = %q, want %q", fact.FactKind, got, want)
		}
		if got, want := fact.SourceConfidence, "observed"; got != want {
			t.Fatalf("fact %q SourceConfidence = %q, want %q", fact.FactKind, got, want)
		}
	}
}

func TestStreamFactsSkipsMissingFile(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	// Do NOT create the file — re-read should skip it gracefully.

	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{
			{
				"lang": "python",
				"path": repoPath + "/missing.py",
			},
		},
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "missing.py",
			Digest:       "digest-missing",
			Language:     "python",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	allFacts := drainFactChannel(collected.Facts)

	// With missing file: 1 repo + 1 file + 0 content (skipped) + 0 entities + 7 followups = 9
	if got, want := len(allFacts), 9; got != want {
		t.Fatalf("fact count = %d, want %d (missing file should be skipped)", got, want)
	}

	for _, f := range allFacts {
		if f.FactKind == "content" {
			t.Fatal("found content fact for missing file, want skip")
		}
	}
}

func newTestCollectorGitSource(
	observedAt time.Time,
	repoPath string,
	snapshot RepositorySnapshot,
) *GitSource {
	return &GitSource{
		Component: "collector-git",
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt: observedAt,
				Repositories: []SelectedRepository{{
					RepoPath:  repoPath,
					RemoteURL: "https://github.com/example/service",
				}},
			}},
		},
		Snapshotter: &stubRepositorySnapshotter{
			snapshots: map[string]RepositorySnapshot{
				repoPath: snapshot,
			},
		},
	}
}

func drainFactChannel(ch <-chan facts.Envelope) []facts.Envelope {
	var result []facts.Envelope
	for f := range ch {
		result = append(result, f)
	}
	return result
}

func testCollectorRepositoryMetadata(repoPath string) repositoryidentity.Metadata {
	return repositoryidentity.Metadata{
		ID:        "repository:r_12345678",
		Name:      "example-service",
		RepoSlug:  "example/service",
		RemoteURL: "https://github.com/example/service",
		LocalPath: repoPath,
		HasRemote: true,
	}
}

func testCollectorSnapshot(repoPath string, body string, digest string) RepositorySnapshot {
	return RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{
			{
				"lang": "python",
				"path": repoPath + "/app.py",
				"functions": []any{
					map[string]any{
						"name":        "handler",
						"line_number": 1,
						"uid":         "content-entity:e_fn123456789",
					},
				},
			},
		},
		ContentFiles: []ContentFileSnapshot{{
			RelativePath: "app.py",
			Body:         body,
			Digest:       digest,
			Language:     "python",
		}},
		ContentEntities: []ContentEntitySnapshot{{
			EntityID:     "content-entity:e_fn123456789",
			RelativePath: "app.py",
			EntityType:   "Function",
			EntityName:   "handler",
			StartLine:    1,
			EndLine:      2,
			Language:     "python",
			SourceCache:  body,
			IndexedAt:    time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC),
		}},
	}
}

// testCollectorSnapshotWithMetas builds a snapshot using ContentFileMetas
// (two-phase path) instead of ContentFiles (legacy path).
func testCollectorSnapshotWithMetas(repoPath string, body string, digest string) RepositorySnapshot {
	return RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{
			{
				"lang": "python",
				"path": repoPath + "/app.py",
				"functions": []any{
					map[string]any{
						"name":        "handler",
						"line_number": 1,
						"uid":         "content-entity:e_fn123456789",
					},
				},
			},
		},
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "app.py",
			Digest:       digest,
			Language:     "python",
		}},
		ContentEntities: []ContentEntitySnapshot{{
			EntityID:     "content-entity:e_fn123456789",
			RelativePath: "app.py",
			EntityType:   "Function",
			EntityName:   "handler",
			StartLine:    1,
			EndLine:      2,
			Language:     "python",
			SourceCache:  body,
			IndexedAt:    time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC),
		}},
	}
}
