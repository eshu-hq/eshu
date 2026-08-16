// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// testServiceURL is the API base URL the orchestration tests pretend to
// target. Nothing dials it: every network-shaped seam is faked.
const testServiceURL = "http://localhost:8080"

// nilClient is the client value handed to Execute in these tests. Every seam
// the tests wire ignores its client argument, so no fake transport is needed;
// a test that adds a seam actually reading the client must supply a real fake.
var nilClient scan.Client

// fakeFirstRunProbe builds a runtime probe with explicit, deterministic seams.
func fakeFirstRunProbe(apiHealthy bool, binaries map[string]bool, files map[string]bool) RuntimeProbe {
	return RuntimeProbe{
		APIHealthy: func(string) bool { return apiHealthy },
		LookPath: func(file string) (string, error) {
			if binaries[file] {
				return "/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
		FileExists: func(path string) bool { return files[path] },
	}
}

func baseFirstRunOptions() Options {
	return Options{
		Path:         ".",
		Timeout:      time.Minute,
		PollInterval: time.Millisecond,
	}
}

// localPathSelectorMatches is the test stand-in for the wrapper-wired
// repository selector: it matches an entry whose LocalPath equals the target.
func localPathSelectorMatches(repo Repository, selector string) bool {
	return repo.LocalPath == selector
}

// TestDetectFirstRunRuntimePrefersReachableAPI proves an already-reachable API
// is the chosen shape even when binaries and compose files are also present.
func TestDetectFirstRunRuntimePrefersReachableAPI(t *testing.T) {
	probe := fakeFirstRunProbe(
		true,
		map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true},
		map[string]bool{"/ws/docker-compose.yaml": true},
	)
	detection := detectFirstRunRuntime(probe, testServiceURL, "/ws")
	if detection.Shape != ShapeExistingAPI {
		t.Fatalf("shape = %q, want existing_api", detection.Shape)
	}
	if !detection.APIReachable {
		t.Fatal("APIReachable = false, want true")
	}
}

// TestDetectFirstRunRuntimeFallsBackToLocalBinaries proves local binaries are
// chosen when the API is down but binaries are on PATH.
func TestDetectFirstRunRuntimeFallsBackToLocalBinaries(t *testing.T) {
	probe := fakeFirstRunProbe(
		false,
		map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true},
		map[string]bool{},
	)
	detection := detectFirstRunRuntime(probe, testServiceURL, "/ws")
	if detection.Shape != ShapeLocalBinaries {
		t.Fatalf("shape = %q, want local_binaries", detection.Shape)
	}
}

// TestDetectFirstRunRuntimeFallsBackToCompose proves a compose file is chosen
// when the API is down and binaries are missing.
func TestDetectFirstRunRuntimeFallsBackToCompose(t *testing.T) {
	probe := fakeFirstRunProbe(
		false,
		map[string]bool{},
		map[string]bool{"/ws/docker-compose.yaml": true},
	)
	detection := detectFirstRunRuntime(probe, testServiceURL, "/ws")
	if detection.Shape != ShapeDockerCompose {
		t.Fatalf("shape = %q, want docker_compose", detection.Shape)
	}
	if detection.ComposeFile != "/ws/docker-compose.yaml" {
		t.Fatalf("ComposeFile = %q, want /ws/docker-compose.yaml", detection.ComposeFile)
	}
}

// TestDetectFirstRunRuntimeUnknownWhenNothingAvailable proves the unknown shape
// when API is down, binaries are missing, and no compose file exists. This is
// the Compose/API unavailable + missing binaries acceptance case.
func TestDetectFirstRunRuntimeUnknownWhenNothingAvailable(t *testing.T) {
	probe := fakeFirstRunProbe(false, map[string]bool{}, map[string]bool{})
	detection := detectFirstRunRuntime(probe, testServiceURL, "/ws")
	if detection.Shape != ShapeUnknown {
		t.Fatalf("shape = %q, want unknown", detection.Shape)
	}
}

// TestExecuteFirstRunFailsWhenRuntimeUnavailable proves first-run does not claim
// success and emits next steps when no runtime is available.
func TestExecuteFirstRunFailsWhenRuntimeUnavailable(t *testing.T) {
	deps := Deps{
		Probe:         fakeFirstRunProbe(false, map[string]bool{}, map[string]bool{}),
		FetchStatus:   func(scan.Client) (scan.PipelineStatus, error) { return scan.PipelineStatus{}, nil },
		ListRepos:     func(scan.Client) (RepositoryList, error) { return RepositoryList{}, nil },
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("Execute() error = nil, want runtime failure")
	}
	if result.succeeded() {
		t.Fatal("result.succeeded() = true, want false")
	}
	if len(result.NextSteps) == 0 {
		t.Fatal("NextSteps empty, want actionable steps")
	}
}

// TestExecuteFirstRunMissingBinariesWithComposeDownFails proves the missing
// binaries + compose unreachable path stays truthful.
func TestExecuteFirstRunMissingBinariesWithComposeDownFails(t *testing.T) {
	deps := Deps{
		Probe:         fakeFirstRunProbe(false, map[string]bool{}, map[string]bool{"/ws/docker-compose.yaml": true}),
		FetchStatus:   func(scan.Client) (scan.PipelineStatus, error) { return scan.PipelineStatus{}, nil },
		ListRepos:     func(scan.Client) (RepositoryList, error) { return RepositoryList{}, nil },
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("Execute() error = nil, want compose-down failure")
	}
	if result.RuntimeShape != ShapeDockerCompose {
		t.Fatalf("RuntimeShape = %q, want docker_compose", result.RuntimeShape)
	}
	if !strings.Contains(err.Error(), "docker compose up") {
		t.Fatalf("error = %q, want compose start hint", err.Error())
	}
}

// TestExecuteFirstRunReusesExistingIndexedRepo proves the existing-API path can
// reuse an already-indexed, drained repository and answer the first query.
func TestExecuteFirstRunReusesExistingIndexedRepo(t *testing.T) {
	deps := Deps{
		Probe: fakeFirstRunProbe(true, map[string]bool{}, map[string]bool{}),
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			return scan.PipelineStatus{
				Health:            scan.Health{State: "healthy"},
				GenerationHistory: scan.GenerationHistory{Completed: 1},
			}, nil
		},
		ListRepos: func(scan.Client) (RepositoryList, error) {
			return RepositoryList{Repositories: []Repository{{ID: "r1", Name: "demo", LocalPath: "/ws"}}}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, scan.Runtime, scan.Options, bool) (scan.Result, error) {
			t.Fatal("RunScan should not be called when a complete index already exists")
			return scan.Result{}, nil
		},
		MatchesSelector: localPathSelectorMatches,
		WorkspaceRoot:   "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !result.succeeded() {
		t.Fatal("result.succeeded() = false, want true")
	}
	if result.RepoIndexed != "complete" {
		t.Fatalf("RepoIndexed = %q, want complete", result.RepoIndexed)
	}
}

// TestExecuteFirstRunSkipsReuseWithoutSelectorSeam proves a missing selector
// seam falls back to a fresh scan instead of reusing an unproven index, so a
// miswired caller can never claim reuse it did not prove.
func TestExecuteFirstRunSkipsReuseWithoutSelectorSeam(t *testing.T) {
	var scanCalled bool
	deps := Deps{
		Probe: fakeFirstRunProbe(true, map[string]bool{}, map[string]bool{}),
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			return scan.PipelineStatus{
				Health:            scan.Health{State: "healthy"},
				GenerationHistory: scan.GenerationHistory{Completed: 1},
			}, nil
		},
		ListRepos: func(scan.Client) (RepositoryList, error) {
			return RepositoryList{Repositories: []Repository{{ID: "r1", Name: "demo", LocalPath: "/ws"}}}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, scan.Runtime, scan.Options, bool) (scan.Result, error) {
			scanCalled = true
			return scan.Result{Status: "ready"}, nil
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	if _, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions()); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !scanCalled {
		t.Fatal("RunScan was not called; a nil MatchesSelector must not reuse an index")
	}
}

// TestExecuteFirstRunRunsScanThenQuery proves the local path runs a scan and
// then a bounded query, reporting success only after the query returns.
func TestExecuteFirstRunRunsScanThenQuery(t *testing.T) {
	var scanCalled bool
	deps := Deps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			// No repositories yet, so detection returns no existing index.
			return scan.PipelineStatus{Health: scan.Health{State: "progressing"}}, nil
		},
		ListRepos: func(scan.Client) (RepositoryList, error) {
			if !scanCalled {
				return RepositoryList{}, nil
			}
			return RepositoryList{Repositories: []Repository{{ID: "r1", Name: "demo"}}}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, scan.Runtime, scan.Options, bool) (scan.Result, error) {
			scanCalled = true
			return scan.Result{Status: "ready"}, nil
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !scanCalled {
		t.Fatal("RunScan was not called")
	}
	if !result.succeeded() {
		t.Fatal("result.succeeded() = false, want true")
	}
	if !strings.Contains(result.QuerySummary, "returned 1") {
		t.Fatalf("QuerySummary = %q, want 1 repository", result.QuerySummary)
	}
}

// TestExecuteFirstRunSurfacesDeadLetterFailure proves dead-letter work during
// indexing fails the run with the root-cause detail preserved.
func TestExecuteFirstRunSurfacesDeadLetterFailure(t *testing.T) {
	deps := Deps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			return scan.PipelineStatus{Health: scan.Health{State: "progressing"}}, nil
		},
		ListRepos: func(scan.Client) (RepositoryList, error) {
			return RepositoryList{}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, scan.Runtime, scan.Options, bool) (scan.Result, error) {
			return scan.Result{
					Status:       "failed",
					StatusReport: scan.PipelineStatus{Queue: scan.Queue{DeadLetter: 2}, Health: scan.Health{State: "degraded"}},
				},
				errors.New("scan readiness timed out: queue has dead-letter work")
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("Execute() error = nil, want dead-letter failure")
	}
	if !strings.Contains(err.Error(), "dead-letter") {
		t.Fatalf("error = %q, want dead-letter detail", err.Error())
	}
	if result.succeeded() {
		t.Fatal("result.succeeded() = true, want false on dead-letter")
	}
	if result.RepoIndexed != "failed" {
		t.Fatalf("RepoIndexed = %q, want failed", result.RepoIndexed)
	}
}

// TestExecuteFirstRunPartialReadinessIsNotSuccess proves a partial scan result
// without a returning query does not report success.
func TestExecuteFirstRunPartialReadinessIsNotSuccess(t *testing.T) {
	deps := Deps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			return scan.PipelineStatus{Health: scan.Health{State: "progressing"}}, nil
		},
		ListRepos: func(scan.Client) (RepositoryList, error) { return RepositoryList{}, nil },
		RunScan: func(context.Context, io.Writer, io.Writer, scan.Runtime, scan.Options, bool) (scan.Result, error) {
			return scan.Result{Status: "partial", StatusReport: scan.PipelineStatus{Health: scan.Health{State: "degraded"}}},
				errors.New("scan readiness timed out: queue still has outstanding work")
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("Execute() error = nil, want partial-readiness failure")
	}
	if result.succeeded() {
		t.Fatal("result.succeeded() = true, want false on partial readiness")
	}
	if result.RepoIndexed != "partial" {
		t.Fatalf("RepoIndexed = %q, want partial", result.RepoIndexed)
	}
}

// TestExecuteFirstRunQueryFailureIsNotSuccess proves a failing bounded query is
// not reported as success even after a clean index.
func TestExecuteFirstRunQueryFailureIsNotSuccess(t *testing.T) {
	var calls int
	deps := Deps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			return scan.PipelineStatus{Health: scan.Health{State: "progressing"}}, nil
		},
		ListRepos: func(scan.Client) (RepositoryList, error) {
			calls++
			// First call is the existing-index probe (empty), final query errors.
			if calls == 1 {
				return RepositoryList{}, nil
			}
			return RepositoryList{}, errors.New("connection refused")
		},
		RunScan: func(context.Context, io.Writer, io.Writer, scan.Runtime, scan.Options, bool) (scan.Result, error) {
			return scan.Result{Status: "ready"}, nil
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("Execute() error = nil, want query failure")
	}
	if !strings.Contains(err.Error(), "first query") {
		t.Fatalf("error = %q, want first query detail", err.Error())
	}
	if result.succeeded() {
		t.Fatal("result.succeeded() = true, want false when query fails")
	}
}

// TestRunFirstRunQueryEmptyListIsTruthfulAnswer proves an empty repository list
// is a valid, returning answer (no repositories found case).
func TestRunFirstRunQueryEmptyListIsTruthfulAnswer(t *testing.T) {
	deps := Deps{
		ListRepos: func(scan.Client) (RepositoryList, error) { return RepositoryList{}, nil },
	}
	answer, err := runFirstRunQuery(deps, nilClient)
	if err != nil {
		t.Fatalf("runFirstRunQuery() error = %v, want nil", err)
	}
	if !strings.Contains(answer, "0 repositories") {
		t.Fatalf("answer = %q, want 0 repositories", answer)
	}
}

// fakeReposDir is a ReposDir seam that avoids touching the real filesystem
// layout so orchestration tests stay hermetic.
func fakeReposDir(root string) (string, error) {
	return root + "/.cache/repos", nil
}
