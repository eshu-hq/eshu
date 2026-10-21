// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// storedLoader copies the production loader with the stored read enabled over
// the fake reader.
func storedLoader(stored *fakeStoredReader) configArtifactLoader {
	loader := productionConfigArtifactLoader
	loader.stored = StoredConfig{Enabled: true, Reader: stored}
	return loader
}

// storedFixtureStore serves the anchor and two related repositories with the
// same files, so any row difference between the live and stored answers is a
// defect, never a fixture asymmetry.
func storedFixtureStore() *fakeContentStore {
	return newFakeContentStore(map[string][]querycontract.FileContent{
		"anchor": loaderFiles(),
		"repo-a": loaderFiles(),
		"repo-b": loaderFiles(),
	})
}

// storedFixtureGraph serves two related repositories with distinct graph
// names, so merged live rows split back per repository on source_repo.
func storedFixtureGraph() *fakeArtifactGraph {
	return &fakeArtifactGraph{related: []repositoryArtifactSource{
		{RepoID: "repo-a", RepoName: "name-a"},
		{RepoID: "repo-b", RepoName: "name-b"},
	}}
}

// mirrorLiveByRepo splits merged live rows by source_repo and reverse-maps
// each repository's rows to the stored rows the derive would have written,
// keyed by repository id.
func mirrorLiveByRepo(t *testing.T, graph *fakeArtifactGraph, live []map[string]any) map[string]StoredRepository {
	t.Helper()
	nameToID := map[string]string{}
	for _, source := range graph.related {
		nameToID[source.RepoName] = source.RepoID
	}
	bySource := map[string][]map[string]any{}
	for _, row := range live {
		source := querycontract.StringVal(row, "source_repo")
		bySource[source] = append(bySource[source], row)
	}
	mirrored := map[string]StoredRepository{}
	for source, rows := range bySource {
		id, ok := nameToID[source]
		if !ok {
			continue
		}
		mirrored[id] = StoredRepository{
			RepoID:  id,
			Trusted: true,
			Rows:    storedRowsMirroringLive(t, rows),
		}
	}
	return mirrored
}

// marshalBody renders an overview for the identical-row comparisons.
func marshalBody(t *testing.T, overview map[string]any) string {
	t.Helper()
	body, err := json.Marshal(overview)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

// TestStoredBodyEqualsLiveBody is the unit identical-row proof (#8155 PR3c):
// every related repository served from stored rows mirroring the live rows
// answers the identical body to the live load, with the anchor live in both.
func TestStoredBodyEqualsLiveBody(t *testing.T) {
	t.Parallel()

	store := storedFixtureStore()
	graph := storedFixtureGraph()
	var liveStats ConfigArtifactReadStats
	liveMerged, err := productionConfigArtifactLoader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &liveStats)
	if err != nil {
		t.Fatalf("live load error = %v", err)
	}
	if liveStats.ReadMode != string(content.ReadModeLive) {
		t.Fatalf("live load ReadMode = %q, want live", liveStats.ReadMode)
	}
	liveRows := querycontract.MapSliceValue(liveMerged, "config_paths")
	if len(liveRows) == 0 {
		t.Fatal("fixture produced no rows; the identical-row check is vacuous")
	}
	stored := &fakeStoredReader{repos: mirrorLiveByRepo(t, graph, liveRows)}
	var storedStats ConfigArtifactReadStats
	storedMerged, err := storedLoader(stored).loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &storedStats)
	if err != nil {
		t.Fatalf("stored load error = %v", err)
	}
	if got, want := marshalBody(t, storedMerged), marshalBody(t, liveMerged); got != want {
		t.Fatalf("stored body differs from the live body:\nstored %s\nlive   %s", got, want)
	}
	if storedStats.ReadMode != string(content.ReadModeStored) {
		t.Fatalf("stored load ReadMode = %q, want stored", storedStats.ReadMode)
	}
	if storedStats.StoredRepositoryCount != 2 || storedStats.LiveRepositoryCount != 0 {
		t.Fatalf("stored=%d live=%d, want stored=2 live=0", storedStats.StoredRepositoryCount, storedStats.LiveRepositoryCount)
	}
	if len(storedStats.LiveReasons) != 0 {
		t.Fatalf("LiveReasons = %v, want empty: no repository fell back", storedStats.LiveReasons)
	}
	if storedStats.StoredRowCount != len(liveRows)-anchorRowCount(t, liveRows) {
		t.Fatalf("StoredRowCount = %d, want the related live row count", storedStats.StoredRowCount)
	}
	if len(stored.calls) != 1 {
		t.Fatalf("stored read issued %d calls, want exactly 1", len(stored.calls))
	}
	if want := []string{"repo-a", "repo-b"}; !reflect.DeepEqual(stored.calls[0], want) {
		t.Fatalf("stored read ids = %v, want %v in walk order", stored.calls[0], want)
	}
}

// anchorRowCount counts the merged live rows the anchor owns.
func anchorRowCount(t *testing.T, live []map[string]any) int {
	t.Helper()
	count := 0
	for _, row := range live {
		if querycontract.StringVal(row, "source_repo") == "svc" {
			count++
		}
	}
	return count
}

// TestStoredFallbackServesLivePerReason pins the per-repository fallback:
// each untrusted related repository loads live with its reason counted,
// trusted ones stay stored, and the body still equals the live body.
func TestStoredFallbackServesLivePerReason(t *testing.T) {
	t.Parallel()

	store := storedFixtureStore()
	graph := storedFixtureGraph()
	var liveStats ConfigArtifactReadStats
	liveMerged, err := productionConfigArtifactLoader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &liveStats)
	if err != nil {
		t.Fatalf("live load error = %v", err)
	}
	liveRows := querycontract.MapSliceValue(liveMerged, "config_paths")
	mirrored := mirrorLiveByRepo(t, graph, liveRows)
	trusted := mirrored["repo-a"]
	stored := &fakeStoredReader{repos: map[string]StoredRepository{
		"repo-a": trusted,
		"repo-b": {RepoID: "repo-b", Trusted: false, Reason: "stale_version"},
	}}
	var stats ConfigArtifactReadStats
	storedMerged, err := storedLoader(stored).loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats)
	if err != nil {
		t.Fatalf("stored load error = %v", err)
	}
	if got, want := marshalBody(t, storedMerged), marshalBody(t, liveMerged); got != want {
		t.Fatalf("fallback body differs from the live body:\nstored %s\nlive   %s", got, want)
	}
	if stats.StoredRepositoryCount != 1 || stats.LiveRepositoryCount != 1 {
		t.Fatalf("stored=%d live=%d, want stored=1 live=1", stats.StoredRepositoryCount, stats.LiveRepositoryCount)
	}
	if want := map[string]int{"stale_version": 1}; !reflect.DeepEqual(stats.LiveReasons, want) {
		t.Fatalf("LiveReasons = %v, want %v", stats.LiveReasons, want)
	}
}

// TestStoredFallbackReasonsCoverMissingAndFailed pins the remaining fallback
// reasons: a repository the stored read drops counts missing, and a failed
// derive counts failed.
func TestStoredFallbackReasonsCoverMissingAndFailed(t *testing.T) {
	t.Parallel()

	store := storedFixtureStore()
	graph := storedFixtureGraph()
	stored := &fakeStoredReader{repos: map[string]StoredRepository{
		"repo-a": {RepoID: "repo-a", Trusted: false, Reason: "failed"},
	}}
	var stats ConfigArtifactReadStats
	if _, err := storedLoader(stored).loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats); err != nil {
		t.Fatalf("stored load error = %v", err)
	}
	want := map[string]int{"failed": 1, "missing": 1}
	if !reflect.DeepEqual(stats.LiveReasons, want) {
		t.Fatalf("LiveReasons = %v, want %v", stats.LiveReasons, want)
	}
	if stats.StoredRepositoryCount != 0 || stats.LiveRepositoryCount != 2 {
		t.Fatalf("stored=%d live=%d, want stored=0 live=2", stats.StoredRepositoryCount, stats.LiveRepositoryCount)
	}
}

// TestStoredWholeReadErrorServesEverythingLive pins the fail-safe: a failed
// stored read serves every repository live with stored_error counted, and an
// enabled stored load with no reader does the same without a statement.
func TestStoredWholeReadErrorServesEverythingLive(t *testing.T) {
	t.Parallel()

	store := storedFixtureStore()
	graph := storedFixtureGraph()
	var liveStats ConfigArtifactReadStats
	liveMerged, err := productionConfigArtifactLoader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &liveStats)
	if err != nil {
		t.Fatalf("live load error = %v", err)
	}
	stored := &fakeStoredReader{readErr: errors.New("connection refused")}
	var stats ConfigArtifactReadStats
	storedMerged, err := storedLoader(stored).loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats)
	if err != nil {
		t.Fatalf("stored load error = %v, want the live fallback, not an error", err)
	}
	if got, want := marshalBody(t, storedMerged), marshalBody(t, liveMerged); got != want {
		t.Fatalf("error-fallback body differs from the live body")
	}
	if want := map[string]int{"stored_error": 2}; !reflect.DeepEqual(stats.LiveReasons, want) {
		t.Fatalf("LiveReasons = %v, want %v", stats.LiveReasons, want)
	}

	loader := productionConfigArtifactLoader
	loader.stored = StoredConfig{Enabled: true}
	var nilStats ConfigArtifactReadStats
	if _, err := loader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &nilStats); err != nil {
		t.Fatalf("nil-reader stored load error = %v, want the live fallback", err)
	}
	if want := map[string]int{"stored_error": 2}; !reflect.DeepEqual(nilStats.LiveReasons, want) {
		t.Fatalf("nil-reader LiveReasons = %v, want %v", nilStats.LiveReasons, want)
	}
	if nilStats.StoredStatements != 0 {
		t.Fatalf("nil-reader StoredStatements = %d, want 0: no statement was issued", nilStats.StoredStatements)
	}
}

// TestStoredReadTakesOnlyGrantFilteredIDs is the access proof (#5167 W3 P0):
// the stored read receives only the related ids the grant filter kept, so an
// out-of-grant repository with stored rows never reaches the body.
func TestStoredReadTakesOnlyGrantFilteredIDs(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{
		"anchor":   loaderFiles(),
		"repo-in":  loaderFiles(),
		"repo-out": loaderFiles(),
	})
	graph := &fakeArtifactGraph{related: []repositoryArtifactSource{
		{RepoID: "repo-out", RepoName: "name-out"},
		{RepoID: "repo-in", RepoName: "name-in"},
	}}
	ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"anchor", "repo-in"},
	})
	stored := &fakeStoredReader{repos: map[string]StoredRepository{
		"repo-out": {RepoID: "repo-out", Trusted: true, Rows: storedRowsMirroringLive(t, shadowLiveRows(t, store, []repositoryArtifactSource{{RepoID: "repo-out", RepoName: "name-out"}}))},
	}}
	var stats ConfigArtifactReadStats
	merged, err := storedLoader(stored).loadShared(ctx, graph, store, "anchor", "svc", loaderFiles(), &stats)
	if err != nil {
		t.Fatalf("stored load error = %v", err)
	}
	if len(stored.calls) != 1 {
		t.Fatalf("stored read issued %d calls, want exactly 1", len(stored.calls))
	}
	if want := []string{"repo-in"}; !reflect.DeepEqual(stored.calls[0], want) {
		t.Fatalf("stored read ids = %v, want %v: the out-of-grant id must not be read", stored.calls[0], want)
	}
	for _, row := range querycontract.MapSliceValue(merged, "config_paths") {
		if querycontract.StringVal(row, "source_repo") == "name-out" {
			t.Fatalf("body holds an out-of-grant row: %v", row)
		}
	}
	if stats.RelatedRepositoryCount != 1 {
		t.Fatalf("RelatedRepositoryCount = %d, want 1 inside the grant", stats.RelatedRepositoryCount)
	}
}

// TestStoredCollapsesSharedGraphNames pins the one-merge contract for two
// related repositories sharing a graph name, and for the anchor repeated in
// the related list: the stored body equals the live body in both cases.
func TestStoredCollapsesSharedGraphNames(t *testing.T) {
	t.Parallel()

	t.Run("shared name", func(t *testing.T) {
		t.Parallel()
		store := newFakeContentStore(map[string][]querycontract.FileContent{
			"anchor": loaderFiles(),
			"repo-a": loaderFiles(),
			"repo-b": loaderFiles(),
		})
		graph := &fakeArtifactGraph{related: []repositoryArtifactSource{
			{RepoID: "repo-a", RepoName: "same"},
			{RepoID: "repo-b", RepoName: "same"},
		}}
		assertStoredEqualsLive(t, store, graph)
	})
	t.Run("anchor in related", func(t *testing.T) {
		t.Parallel()
		store := storedFixtureStore()
		graph := &fakeArtifactGraph{related: []repositoryArtifactSource{
			{RepoID: "anchor", RepoName: "svc"},
			{RepoID: "repo-a", RepoName: "name-a"},
		}}
		assertStoredEqualsLive(t, store, graph)
	})
}

// assertStoredEqualsLive mirrors every related repository's live rows into
// the fake stored reader and requires the stored body to equal the live body.
func assertStoredEqualsLive(t *testing.T, store *fakeContentStore, graph *fakeArtifactGraph) {
	t.Helper()
	var liveStats ConfigArtifactReadStats
	liveMerged, err := productionConfigArtifactLoader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &liveStats)
	if err != nil {
		t.Fatalf("live load error = %v", err)
	}
	liveRows := querycontract.MapSliceValue(liveMerged, "config_paths")
	if len(liveRows) == 0 {
		t.Fatal("fixture produced no rows; the identical-row check is vacuous")
	}
	stored := &fakeStoredReader{repos: mirrorLiveByRepo(t, graph, liveRows)}
	var stats ConfigArtifactReadStats
	storedMerged, err := storedLoader(stored).loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats)
	if err != nil {
		t.Fatalf("stored load error = %v", err)
	}
	if got, want := marshalBody(t, storedMerged), marshalBody(t, liveMerged); got != want {
		t.Fatalf("stored body differs from the live body:\nstored %s\nlive   %s", got, want)
	}
}

// TestStoredWindowReasonComesFromListTruncated pins the stored 5,000-window
// disclosure: a trusted truncated repository adds the window reason with no
// budget reason, even though nothing was listed or hydrated for it.
func TestStoredWindowReasonComesFromListTruncated(t *testing.T) {
	t.Parallel()

	store := storedFixtureStore()
	graph := storedFixtureGraph()
	stored := &fakeStoredReader{repos: map[string]StoredRepository{
		"repo-a": {RepoID: "repo-a", Trusted: true, ListTruncated: true},
		"repo-b": {RepoID: "repo-b", Trusted: true},
	}}
	var stats ConfigArtifactReadStats
	if _, err := storedLoader(stored).loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats); err != nil {
		t.Fatalf("stored load error = %v", err)
	}
	if stats.StoredRepositoriesListTruncated != 1 {
		t.Fatalf("StoredRepositoriesListTruncated = %d, want 1", stats.StoredRepositoriesListTruncated)
	}
	reasons := stats.BoundReasons()
	if len(reasons) != 1 || reasons[0] != RelatedFileListTruncatedReason {
		t.Fatalf("BoundReasons = %v, want exactly the window reason", reasons)
	}
	if stats.CandidatesUnhydrated != 0 || stats.RepositoriesBudgetCapped != 0 {
		t.Fatalf("unhydrated=%d capped=%d, want zero: a stored repository never emits the budget reason",
			stats.CandidatesUnhydrated, stats.RepositoriesBudgetCapped)
	}
}

// TestStoredSkipsShadow pins the mode precedence: with both configured the
// stored load wins and the shadow compare does not run.
func TestStoredSkipsShadow(t *testing.T) {
	t.Parallel()

	store := storedFixtureStore()
	graph := storedFixtureGraph()
	stored := &fakeStoredReader{repos: map[string]StoredRepository{}}
	var reported []ShadowReport
	loader := storedLoader(stored)
	loader.shadow = ShadowConfig{Enabled: true, Reader: stored, Reporter: func(_ context.Context, r ShadowReport) {
		reported = append(reported, r)
	}}
	var stats ConfigArtifactReadStats
	if _, err := loader.loadShared(context.Background(), graph, store, "anchor", "svc", loaderFiles(), &stats); err != nil {
		t.Fatalf("stored load error = %v", err)
	}
	if len(reported) != 0 {
		t.Fatalf("shadow reported %d reports on a stored load, want none", len(reported))
	}
	if len(stored.calls) != 1 {
		t.Fatalf("stored read issued %d calls, want exactly 1 (the stored load, not the shadow)", len(stored.calls))
	}
}

// TestLiveReasonsSummaryRendersSortedPairs pins the stage live_reasons
// rendering: reasons sort by name as reason=count pairs, empty when none.
func TestLiveReasonsSummaryRendersSortedPairs(t *testing.T) {
	t.Parallel()

	stats := ConfigArtifactReadStats{LiveReasons: map[string]int{"stale_version": 1, "missing": 2}}
	if got, want := stats.LiveReasonsSummary(), "missing=2,stale_version=1"; got != want {
		t.Fatalf("LiveReasonsSummary() = %q, want %q", got, want)
	}
	if got := (ConfigArtifactReadStats{}).LiveReasonsSummary(); got != "" {
		t.Fatalf("empty LiveReasonsSummary() = %q, want empty", got)
	}
}

// TestConfigureProductionReads pins the API-start wiring: each Configure
// call enables its mode on the production loader with the given reader and
// reporter, and the other mode stays disabled.
func TestConfigureProductionReads(t *testing.T) {
	saved := productionConfigArtifactLoader
	t.Cleanup(func() { productionConfigArtifactLoader = saved })

	stored := &fakeStoredReader{}
	ConfigureProductionStoredReads(stored)
	if !productionConfigArtifactLoader.stored.Enabled {
		t.Fatal("ConfigureProductionStoredReads left stored disabled")
	}
	if productionConfigArtifactLoader.stored.Reader != StoredReader(stored) {
		t.Fatal("ConfigureProductionStoredReads did not keep the reader")
	}
	if productionConfigArtifactLoader.shadow.Enabled {
		t.Fatal("ConfigureProductionStoredReads enabled the shadow compare")
	}

	productionConfigArtifactLoader = saved
	reported := 0
	ConfigureProductionShadowReads(stored, func(context.Context, ShadowReport) { reported++ })
	if !productionConfigArtifactLoader.shadow.Enabled {
		t.Fatal("ConfigureProductionShadowReads left shadow disabled")
	}
	if productionConfigArtifactLoader.shadow.Reader != StoredReader(stored) {
		t.Fatal("ConfigureProductionShadowReads did not keep the reader")
	}
	if productionConfigArtifactLoader.shadow.Reporter == nil {
		t.Fatal("ConfigureProductionShadowReads did not keep the reporter")
	}
	if productionConfigArtifactLoader.stored.Enabled {
		t.Fatal("ConfigureProductionShadowReads enabled the stored load")
	}
	productionConfigArtifactLoader.shadow.Reporter(context.Background(), ShadowReport{})
	if reported != 1 {
		t.Fatal("production shadow reporter was not called")
	}
}
