// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"errors"
	"testing"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// fakeStoredReader is a scripted StoredReader for the shadow tests. It serves
// one StoredRepository per id, records the ids it was asked for, and fails
// with readErr when set.
type fakeStoredReader struct {
	repos   map[string]StoredRepository
	calls   [][]string
	readErr error
}

func (f *fakeStoredReader) ReadStored(_ context.Context, repoIDs []string) ([]StoredRepository, error) {
	f.calls = append(f.calls, repoIDs)
	if f.readErr != nil {
		return nil, f.readErr
	}
	out := make([]StoredRepository, 0, len(repoIDs))
	for _, id := range repoIDs {
		if repo, ok := f.repos[id]; ok {
			out = append(out, repo)
		}
	}
	return out, nil
}

// fakeArtifactGraph serves one fixed related-repository set.
type fakeArtifactGraph struct {
	related []repositoryArtifactSource
	err     error
}

func (f *fakeArtifactGraph) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	rows := make([]map[string]any, 0, len(f.related))
	for _, source := range f.related {
		rows = append(rows, map[string]any{"repo_id": source.RepoID, "repo_name": source.RepoName})
	}
	return rows, nil
}

func (f *fakeArtifactGraph) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

// shadowLiveRows loads two related repositories through the production
// per-repository function and returns the merged live rows.
func shadowLiveRows(t *testing.T, store *fakeContentStore, sources []repositoryArtifactSource) []map[string]any {
	t.Helper()
	var stats ConfigArtifactReadStats
	got, err := loadRepositoryConfigArtifactsForSources(context.Background(), store, sources, &stats)
	if err != nil {
		t.Fatalf("live load error = %v", err)
	}
	rows := querycontract.MapSliceValue(got, "config_paths")
	if len(rows) == 0 {
		t.Fatal("fixture produced no rows; the shadow check is vacuous")
	}
	return rows
}

// storedRowsMirroringLive reverse-maps live rows to stored rows. It holds
// only when the read-time name step is the identity over the fixture: no
// fixture path holds the graph name as a component, so the stripped live
// path equals the stored raw path. The mismatch tests break this mapping on
// purpose.
func storedRowsMirroringLive(t *testing.T, live []map[string]any) []contentartifacts.StoredRow {
	t.Helper()
	rows := make([]contentartifacts.StoredRow, 0, len(live))
	for _, row := range live {
		kind := querycontract.StringVal(row, "evidence_kind")
		scoped, known := contentartifacts.NameScoped(kind)
		if !known {
			t.Fatalf("live row has unknown evidence kind %q", kind)
		}
		rows = append(rows, contentartifacts.StoredRow{
			RawPath:      querycontract.StringVal(row, "path"),
			RelativePath: querycontract.StringVal(row, "relative_path"),
			EvidenceKind: kind,
			NameScoped:   scoped,
		})
	}
	return rows
}

func shadowLoader(stored *fakeStoredReader, reported *[]ShadowReport) configArtifactLoader {
	loader := productionConfigArtifactLoader
	loader.shadow = ShadowConfig{
		Enabled:  true,
		Reader:   stored,
		Reporter: func(r ShadowReport) { *reported = append(*reported, r) },
	}
	return loader
}

// TestShadowNilReporterIssuesNoStoredStatement pins the misconfiguration
// guard: an enabled shadow with no reporter skips the stored read entirely,
// so it costs no statement while still serving the live answer.
func TestShadowNilReporterIssuesNoStoredStatement(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{
		"anchor": loaderFiles(),
		"repo-a": loaderFiles(),
	})
	graph := &fakeArtifactGraph{related: []repositoryArtifactSource{
		{RepoID: "repo-a", RepoName: "name-a"},
	}}
	stored := &fakeStoredReader{}
	loader := productionConfigArtifactLoader
	loader.shadow = ShadowConfig{Enabled: true, Reader: stored, Reporter: nil}

	var stats ConfigArtifactReadStats
	shadowed, err := loader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats)
	if err != nil {
		t.Fatalf("shadow load error = %v", err)
	}
	var liveStats ConfigArtifactReadStats
	liveMerged, err := productionConfigArtifactLoader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &liveStats)
	if err != nil {
		t.Fatalf("live load error = %v", err)
	}
	if got, want := querycontract.MapSliceValue(shadowed, "config_paths"), querycontract.MapSliceValue(liveMerged, "config_paths"); !equalRowLists(got, want) {
		t.Fatalf("nil-reporter shadow changed the merged rows:\n got: %v\nwant: %v", got, want)
	}
	if len(stored.calls) != 0 {
		t.Fatalf("nil-reporter shadow issued %d stored reads, want none", len(stored.calls))
	}
}

// TestShadowServesLiveWhileReportingMatch shadows two related repositories
// whose stored rows mirror the live rows: the merged answer is the live
// answer and the report counts two matches.
func TestShadowServesLiveWhileReportingMatch(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{
		"anchor": loaderFiles(),
		"repo-a": loaderFiles(),
		"repo-b": loaderFiles(),
	})
	graph := &fakeArtifactGraph{related: []repositoryArtifactSource{
		{RepoID: "repo-a", RepoName: "name-a"},
		{RepoID: "repo-b", RepoName: "name-b"},
	}}
	live := shadowLiveRows(t, store, []repositoryArtifactSource{
		{RepoID: "repo-a", RepoName: "name-a"},
		{RepoID: "repo-b", RepoName: "name-b"},
	})
	stored := &fakeStoredReader{repos: map[string]StoredRepository{
		"repo-a": {RepoID: "repo-a", Trusted: true, Rows: storedRowsMirroringLive(t, liveForRepo(t, live, "name-a"))},
		"repo-b": {RepoID: "repo-b", Trusted: true, Rows: storedRowsMirroringLive(t, liveForRepo(t, live, "name-b"))},
	}}
	var reported []ShadowReport
	loader := shadowLoader(stored, &reported)

	var stats ConfigArtifactReadStats
	shadowed, err := loader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats)
	if err != nil {
		t.Fatalf("shadow load error = %v", err)
	}
	var liveStats ConfigArtifactReadStats
	liveMerged, err := productionConfigArtifactLoader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &liveStats)
	if err != nil {
		t.Fatalf("live load error = %v", err)
	}
	// Shadow never changes the response: both loads walk the same live path.
	if got, want := querycontract.MapSliceValue(shadowed, "config_paths"), querycontract.MapSliceValue(liveMerged, "config_paths"); !equalRowLists(got, want) {
		t.Fatalf("shadow changed the merged rows:\n got: %v\nwant: %v", got, want)
	}
	if len(reported) != 1 {
		t.Fatalf("reporter received %d reports, want 1", len(reported))
	}
	report := reported[0]
	if report.ReadErr != nil {
		t.Fatalf("report carries read error %v, want none", report.ReadErr)
	}
	if got := report.Count(contentartifacts.ShadowOutcomeMatch); got != 2 {
		t.Fatalf("report counts %d matches, want 2: %+v", got, report.Repositories)
	}
	if got := report.Count(contentartifacts.ShadowOutcomeMismatch); got != 0 {
		t.Fatalf("report counts %d mismatches, want 0", got)
	}
	// The stored read covers exactly the related repositories, never the
	// anchor, in walk order.
	if len(stored.calls) != 1 || len(stored.calls[0]) != 2 || stored.calls[0][0] != "repo-a" || stored.calls[0][1] != "repo-b" {
		t.Fatalf("stored read ids = %v, want one call with [repo-a repo-b]", stored.calls)
	}
}

// TestShadowReportsMismatchWithoutChangingTheAnswer breaks one stored row:
// the response stays the live answer and the report counts the mismatch.
func TestShadowReportsMismatchWithoutChangingTheAnswer(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{
		"anchor": loaderFiles(),
		"repo-a": loaderFiles(),
	})
	graph := &fakeArtifactGraph{related: []repositoryArtifactSource{{RepoID: "repo-a", RepoName: "svc"}}}
	live := shadowLiveRows(t, store, []repositoryArtifactSource{{RepoID: "repo-a", RepoName: "svc"}})
	mirrored := storedRowsMirroringLive(t, liveForRepo(t, live, "svc"))
	mirrored[0].RawPath = "drifted/renamed.hcl"
	stored := &fakeStoredReader{repos: map[string]StoredRepository{
		"repo-a": {RepoID: "repo-a", Trusted: true, Rows: mirrored},
	}}
	var reported []ShadowReport
	loader := shadowLoader(stored, &reported)

	var stats ConfigArtifactReadStats
	shadowed, err := loader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats)
	if err != nil {
		t.Fatalf("shadow load error = %v", err)
	}
	var liveStats ConfigArtifactReadStats
	liveMerged, err := productionConfigArtifactLoader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &liveStats)
	if err != nil {
		t.Fatalf("live load error = %v", err)
	}
	if got, want := querycontract.MapSliceValue(shadowed, "config_paths"), querycontract.MapSliceValue(liveMerged, "config_paths"); !equalRowLists(got, want) {
		t.Fatal("shadow changed the merged rows on a mismatch; it must serve live")
	}
	if len(reported) != 1 {
		t.Fatalf("reporter received %d reports, want 1", len(reported))
	}
	if got := reported[0].Count(contentartifacts.ShadowOutcomeMismatch); got != 1 {
		t.Fatalf("report counts %d mismatches, want 1: %+v", got, reported[0].Repositories)
	}
}

// TestShadowCountsUntrustedRepositoriesAsStoredMissing covers the repositories
// the stored read cannot vouch for: untrusted and absent alike. Neither is a
// mismatch, and the response stays live.
func TestShadowCountsUntrustedRepositoriesAsStoredMissing(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{
		"anchor": loaderFiles(),
		"repo-a": loaderFiles(),
		"repo-b": loaderFiles(),
	})
	graph := &fakeArtifactGraph{related: []repositoryArtifactSource{
		{RepoID: "repo-a", RepoName: "svc"},
		{RepoID: "repo-b", RepoName: "svc"},
	}}
	stored := &fakeStoredReader{repos: map[string]StoredRepository{
		"repo-a": {RepoID: "repo-a", Trusted: false},
	}}
	var reported []ShadowReport
	loader := shadowLoader(stored, &reported)

	var stats ConfigArtifactReadStats
	if _, err := loader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats); err != nil {
		t.Fatalf("shadow load error = %v", err)
	}
	if len(reported) != 1 {
		t.Fatalf("reporter received %d reports, want 1", len(reported))
	}
	if got := reported[0].Count(contentartifacts.ShadowOutcomeStoredMissing); got != 2 {
		t.Fatalf("report counts %d stored_missing, want 2: %+v", got, reported[0].Repositories)
	}
	if got := reported[0].Count(contentartifacts.ShadowOutcomeMismatch); got != 0 {
		t.Fatalf("report counts %d mismatches, want 0", got)
	}
}

// TestShadowReadErrorKeepsServingLive fails the stored read: the request
// still answers live and the report carries the error with no outcomes.
func TestShadowReadErrorKeepsServingLive(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{
		"anchor": loaderFiles(),
		"repo-a": loaderFiles(),
	})
	graph := &fakeArtifactGraph{related: []repositoryArtifactSource{{RepoID: "repo-a", RepoName: "svc"}}}
	stored := &fakeStoredReader{readErr: errors.New("replica refused")}
	var reported []ShadowReport
	loader := shadowLoader(stored, &reported)

	var stats ConfigArtifactReadStats
	shadowed, err := loader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats)
	if err != nil {
		t.Fatalf("shadow load error = %v, want the live answer despite the stored failure", err)
	}
	if len(querycontract.MapSliceValue(shadowed, "config_paths")) == 0 {
		t.Fatal("shadow load answered empty on a stored failure; it must serve live")
	}
	if len(reported) != 1 || reported[0].ReadErr == nil {
		t.Fatalf("report = %+v, want one report carrying the read error", reported)
	}
	if len(reported[0].Repositories) != 0 {
		t.Fatalf("report carries %d outcomes past a read error, want none", len(reported[0].Repositories))
	}
}

// TestDisabledShadowIssuesNoStoredStatement proves the live default costs
// nothing: with the zero ShadowConfig the loader never calls the stored
// reader, even a failing one.
func TestDisabledShadowIssuesNoStoredStatement(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{
		"anchor": loaderFiles(),
		"repo-a": loaderFiles(),
	})
	graph := &fakeArtifactGraph{related: []repositoryArtifactSource{{RepoID: "repo-a", RepoName: "svc"}}}
	stored := &fakeStoredReader{readErr: errors.New("must not be called")}

	var stats ConfigArtifactReadStats
	if _, err := productionConfigArtifactLoader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats); err != nil {
		t.Fatalf("live load error = %v", err)
	}
	if len(stored.calls) != 0 {
		t.Fatalf("disabled shadow issued %d stored statements, want none", len(stored.calls))
	}
}

// liveForRepo filters merged rows to one graph name.
func liveForRepo(t *testing.T, rows []map[string]any, repoName string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, row := range rows {
		if querycontract.StringVal(row, "source_repo") == repoName {
			out = append(out, row)
		}
	}
	return out
}

// equalRowLists compares two merged row lists element-wise.
func equalRowLists(first, second []map[string]any) bool {
	if len(first) != len(second) {
		return false
	}
	for i := range first {
		if contentartifacts.CompareConfigPaths(first[i], second[i]) != 0 {
			return false
		}
	}
	return true
}
