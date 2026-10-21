// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// These tests flip the production loader's read mode, so they are serial:
// no t.Parallel here. Each resets the mode on cleanup, so the parallel
// disclosure tests that follow still read live.

// storedRouteFake is a scripted leaf StoredReader for the route tests. It
// serves one StoredRepository per id, records the ids it was asked for, and
// fails the whole read with readErr when set.
type storedRouteFake struct {
	repos   map[string]artifacts.StoredRepository
	calls   [][]string
	readErr error
}

func (f *storedRouteFake) ReadStored(_ context.Context, repoIDs []string) ([]artifacts.StoredRepository, error) {
	f.calls = append(f.calls, repoIDs)
	if f.readErr != nil {
		return nil, f.readErr
	}
	out := make([]artifacts.StoredRepository, 0, len(repoIDs))
	for _, id := range repoIDs {
		if repo, ok := f.repos[id]; ok {
			out = append(out, repo)
		}
	}
	return out, nil
}

type storedRouteRun struct {
	body   map[string]any
	logs   string
	reads  map[string]int64
	shadow map[string]int64
}

// storedRouteFiles are extractor-shaped files served for every related
// repository: a Terraform module block and vars, plus an Ansible vars file.
// Every file yields config rows, so the identical-row checks are never
// vacuous, and no path holds a graph repository name.
func storedRouteFiles(repoID string) []querycontract.FileContent {
	return []querycontract.FileContent{
		{RepoID: repoID, RelativePath: "infra/main.tf", Content: "module \"x\" {\n  source = \"./modules/x\"\n}\n"},
		{RepoID: repoID, RelativePath: "infra/prod.tfvars", Content: "region = \"us-east-1\"\n"},
		{RepoID: repoID, RelativePath: "group_vars/all.yml", Content: "key: value\n"},
	}
}

// storedRouteContent serves the anchor from the embedded fake and every
// related repository from the extractor-shaped files.
type storedRouteContent struct {
	metadataOnlyContent
	related map[string]int
}

func (s storedRouteContent) relatedFiles(repoID string, withContent bool) []querycontract.FileContent {
	if _, ok := s.related[repoID]; !ok {
		return nil
	}
	files := storedRouteFiles(repoID)
	if !withContent {
		for i := range files {
			files[i].Content = ""
		}
	}
	return files
}

func (s storedRouteContent) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	if files := s.relatedFiles(repoID, false); files != nil {
		if limit > 0 && len(files) > limit {
			files = files[:limit]
		}
		return files, nil
	}
	return s.metadataOnlyContent.ListRepoFiles(ctx, repoID, limit)
}

func (s storedRouteContent) GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	for _, file := range s.relatedFiles(repoID, true) {
		if file.RelativePath == relativePath {
			file := file
			return &file, nil
		}
	}
	return s.metadataOnlyContent.GetFileContent(ctx, repoID, relativePath)
}

func (s storedRouteContent) GetFileContents(ctx context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	if _, ok := s.related[repoID]; !ok {
		return s.metadataOnlyContent.GetFileContents(ctx, repoID, paths)
	}
	byPath := map[string]querycontract.FileContent{}
	for _, file := range storedRouteFiles(repoID) {
		byPath[file.RelativePath] = file
	}
	files := make([]querycontract.FileContent, 0, len(paths))
	for _, path := range paths {
		if file, ok := byPath[path]; ok {
			files = append(files, file)
		}
	}
	return files, nil
}

func (s storedRouteContent) NonBlankRepoFilePaths(ctx context.Context, repoID string, paths []string) ([]string, error) {
	if _, ok := s.related[repoID]; !ok {
		return s.metadataOnlyContent.NonBlankRepoFilePaths(ctx, repoID, paths)
	}
	return append([]string(nil), paths...), nil
}

// runStoredStory serves the repository story route over two related
// repositories of three YAML candidates each, with the production loader's
// mode set by configure after the handler exists (so the shadow reporter
// closes over the run's instruments and logger), and returns the unwrapped
// body with the logs and the reads/shadow counter points.
func runStoredStory(t *testing.T, related map[string]int, configure func(instruments *telemetry.Instruments, logger *slog.Logger)) storedRouteRun {
	t.Helper()
	artifacts.ResetProductionReads()
	t.Cleanup(artifacts.ResetProductionReads)

	instruments, reader := testMeter(t)
	store := storedRouteContent{
		metadataOnlyContent: metadataOnlyContent{FakePortContentStore: content.FakePortContentStore{RepoFiles: deploymentArtifactsContextFiles()}},
		related:             related,
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := &Handler{
		Neo4j:       relatedGraphReader(related),
		Content:     store,
		Logger:      logger,
		Instruments: instruments,
	}
	configure(instruments, logger)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-da-1/story", nil)
	req.SetPathValue("repo_id", "repo-da-1")
	rec := httptest.NewRecorder()
	handler.getRepositoryStory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("story status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data, ok := body["data"].(map[string]any); ok {
		body = data
	}
	points := collectPoints(t, reader,
		"eshu_dp_config_artifact_reads_total",
		"eshu_dp_config_artifact_shadow_compare_total",
	)
	return storedRouteRun{
		body:   body,
		logs:   logs.String(),
		reads:  points["eshu_dp_config_artifact_reads_total"],
		shadow: points["eshu_dp_config_artifact_shadow_compare_total"],
	}
}

// storyConfigRows returns the story body's config_paths rows.
func storyConfigRows(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	overview, _ := body["infrastructure_overview"].(map[string]any)
	deployment, _ := overview["deployment_artifacts"].(map[string]any)
	rows, _ := deployment["config_paths"].([]any)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if typed, ok := row.(map[string]any); ok {
			out = append(out, typed)
		}
	}
	return out
}

// mirrorStoryRows reverse-maps the story body's related rows to the stored
// rows the derive would have written, keyed by repository id. It fails when
// the read-time name step is not the identity over the fixture, so the
// mirror cannot silently forgive a name-step defect.
func mirrorStoryRows(t *testing.T, rows []map[string]any) map[string]artifacts.StoredRepository {
	t.Helper()
	byName := map[string][]map[string]any{}
	for _, row := range rows {
		source := querycontract.StringVal(row, "source_repo")
		byName[source] = append(byName[source], row)
	}
	mirrored := map[string]artifacts.StoredRepository{}
	for source, sourceRows := range byName {
		if !strings.HasPrefix(source, "name-repo-") {
			continue
		}
		id := strings.TrimPrefix(source, "name-")
		stored := make([]contentartifacts.StoredRow, 0, len(sourceRows))
		for _, row := range sourceRows {
			kind := querycontract.StringVal(row, "evidence_kind")
			scoped, known := contentartifacts.NameScoped(kind)
			if !known {
				t.Fatalf("story row has unknown evidence kind %q", kind)
			}
			path := querycontract.StringVal(row, "path")
			if stripped := contentartifacts.StripRepositoryName(path, source); stripped != path {
				t.Fatalf("story path %q holds the graph name: the mirror is unfaithful", path)
			}
			stored = append(stored, contentartifacts.StoredRow{
				RawPath:      path,
				RelativePath: querycontract.StringVal(row, "relative_path"),
				EvidenceKind: kind,
				NameScoped:   scoped,
			})
		}
		mirrored[id] = artifacts.StoredRepository{RepoID: id, Trusted: true, Rows: stored}
	}
	return mirrored
}

// relatedRowCount counts the story body's rows owned by the related set.
func relatedRowCount(rows []map[string]any) int {
	count := 0
	for _, row := range rows {
		if strings.HasPrefix(querycontract.StringVal(row, "source_repo"), "name-repo-") {
			count++
		}
	}
	return count
}

// liveStoryBody serves the story with the live read and requires related
// rows, so the identical-body comparisons below are never vacuous.
func liveStoryBody(t *testing.T, related map[string]int) map[string]any {
	t.Helper()
	run := runStoredStory(t, related, func(*telemetry.Instruments, *slog.Logger) {})
	rows := storyConfigRows(t, run.body)
	if relatedRowCount(rows) == 0 {
		t.Fatal("live story produced no related rows; the identical-row check is vacuous")
	}
	return run.body
}

// TestStoryServesStoredRowsIdentically is the route identical-row proof
// (#8155 PR3c): with every related repository trusted, the story answers the
// live body from stored rows, names the mode on the stage line, and counts
// the servings.
func TestStoryServesStoredRowsIdentically(t *testing.T) {
	related := map[string]int{"repo-a": 3, "repo-b": 3}
	liveBody := liveStoryBody(t, related)
	mirrored := mirrorStoryRows(t, storyConfigRows(t, liveBody))
	fake := &storedRouteFake{repos: mirrored}
	run := runStoredStory(t, related, func(*telemetry.Instruments, *slog.Logger) {
		artifacts.ConfigureProductionStoredReads(fake)
	})

	live, err := json.Marshal(liveBody)
	if err != nil {
		t.Fatalf("marshal live: %v", err)
	}
	stored, err := json.Marshal(run.body)
	if err != nil {
		t.Fatalf("marshal stored: %v", err)
	}
	if string(stored) != string(live) {
		t.Fatal("stored story body differs from the live body")
	}
	requireLineFields(t,
		deploymentArtifactsStageLine(t, run.logs, "repository_query.stage_completed"),
		[]string{"read_mode=stored", "related_repository_count=2", "stored_repository_count=2", "live_repository_count=0", "stored_statements=1"},
	)
	if got, want := run.reads["reason=trusted,source=stored"], int64(2); got != want {
		t.Fatalf("reads_total trusted/stored = %d, want %d (all points %v)", got, want, run.reads)
	}
}

// TestStoryStoredFallbackPerReason pins the route fallback per reason: a
// related repository the stored read does not vouch for loads live, the body
// stays identical, and the reason is counted. The whole-read error serves
// every repository live as stored_error.
func TestStoryStoredFallbackPerReason(t *testing.T) {
	related := map[string]int{"repo-a": 3, "repo-b": 3}
	liveBody := liveStoryBody(t, related)
	mirrored := mirrorStoryRows(t, storyConfigRows(t, liveBody))

	cases := []struct {
		name   string
		fake   *storedRouteFake
		reason string
	}{
		{name: "missing", fake: &storedRouteFake{repos: map[string]artifacts.StoredRepository{"repo-a": mirrored["repo-a"]}}, reason: "missing=1"},
		{name: "stale version", fake: &storedRouteFake{repos: map[string]artifacts.StoredRepository{
			"repo-a": mirrored["repo-a"],
			"repo-b": {RepoID: "repo-b", Trusted: false, Reason: "stale_version"},
		}}, reason: "stale_version=1"},
		{name: "stale extractor", fake: &storedRouteFake{repos: map[string]artifacts.StoredRepository{
			"repo-a": mirrored["repo-a"],
			"repo-b": {RepoID: "repo-b", Trusted: false, Reason: "stale_extractor"},
		}}, reason: "stale_extractor=1"},
		{name: "failed", fake: &storedRouteFake{repos: map[string]artifacts.StoredRepository{
			"repo-a": mirrored["repo-a"],
			"repo-b": {RepoID: "repo-b", Trusted: false, Reason: "failed"},
		}}, reason: "failed=1"},
		{name: "stored error", fake: &storedRouteFake{readErr: errors.New("connection refused")}, reason: "stored_error=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := tc.fake
			run := runStoredStory(t, related, func(*telemetry.Instruments, *slog.Logger) {
				artifacts.ConfigureProductionStoredReads(fake)
			})
			live, _ := json.Marshal(liveBody)
			stored, _ := json.Marshal(run.body)
			if string(stored) != string(live) {
				t.Fatalf("%s: fallback story body differs from the live body", tc.name)
			}
			line := deploymentArtifactsStageLine(t, run.logs, "repository_query.stage_completed")
			requireLineFields(t, line, []string{"read_mode=stored"})
			if !strings.Contains(line, tc.reason) {
				t.Fatalf("%s: stage line = %q, want it to carry %q", tc.name, line, tc.reason)
			}
			if len(run.reads) == 0 {
				t.Fatalf("%s: reads_total has no points", tc.name)
			}
		})
	}
}

// TestStoryShadowCompareMatchesLive pins the production shadow wiring: the
// story serves the live body, counts the matches, and logs no warning. A
// planted mismatch still serves live, counts the mismatch, and warns.
func TestStoryShadowCompareMatchesLive(t *testing.T) {
	related := map[string]int{"repo-a": 3, "repo-b": 3}
	liveBody := liveStoryBody(t, related)
	mirrored := mirrorStoryRows(t, storyConfigRows(t, liveBody))

	t.Run("match", func(t *testing.T) {
		fake := &storedRouteFake{repos: mirrored}
		run := runStoredStory(t, related, func(instruments *telemetry.Instruments, logger *slog.Logger) {
			artifacts.ConfigureProductionShadowReads(fake, func(ctx context.Context, report artifacts.ShadowReport) {
				ReportConfigArtifactShadow(ctx, instruments, logger, report)
			})
		})
		live, _ := json.Marshal(liveBody)
		shadowed, _ := json.Marshal(run.body)
		if string(shadowed) != string(live) {
			t.Fatal("shadow story body differs from the live body")
		}
		if got := run.shadow["outcome=match"]; got != 2 {
			t.Fatalf("shadow match points = %d, want 2 (all points %v)", got, run.shadow)
		}
		if strings.Contains(run.logs, "config_artifacts.shadow.mismatch") {
			t.Fatalf("shadow log warns on a clean compare:\n%s", run.logs)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		tampered := map[string]artifacts.StoredRepository{}
		for id, repo := range mirrored {
			tampered[id] = repo
		}
		rows := append([]contentartifacts.StoredRow(nil), tampered["repo-b"].Rows...)
		rows = rows[:len(rows)-1]
		repo := tampered["repo-b"]
		repo.Rows = rows
		tampered["repo-b"] = repo
		fake := &storedRouteFake{repos: tampered}
		run := runStoredStory(t, related, func(instruments *telemetry.Instruments, logger *slog.Logger) {
			artifacts.ConfigureProductionShadowReads(fake, func(ctx context.Context, report artifacts.ShadowReport) {
				ReportConfigArtifactShadow(ctx, instruments, logger, report)
			})
		})
		live, _ := json.Marshal(liveBody)
		shadowed, _ := json.Marshal(run.body)
		if string(shadowed) != string(live) {
			t.Fatal("mismatched shadow still serves the live body, but it differs")
		}
		if got := run.shadow["outcome=mismatch"]; got != 1 {
			t.Fatalf("shadow mismatch points = %d, want 1 (all points %v)", got, run.shadow)
		}
		if !strings.Contains(run.logs, "config_artifacts.shadow.mismatch") || !strings.Contains(run.logs, "repo-b") {
			t.Fatalf("shadow log must name the mismatch and repo-b:\n%s", run.logs)
		}
	})
}
