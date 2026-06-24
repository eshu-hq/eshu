package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// fakeFirstRunProbe builds a runtime probe with explicit, deterministic seams.
func fakeFirstRunProbe(apiHealthy bool, binaries map[string]bool, files map[string]bool) firstRunRuntimeProbe {
	return firstRunRuntimeProbe{
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

func baseFirstRunOptions() firstRunOptions {
	return firstRunOptions{
		Path:         ".",
		Timeout:      time.Minute,
		PollInterval: time.Millisecond,
	}
}

func newFirstRunClient() *APIClient {
	return &APIClient{BaseURL: "http://localhost:8080"}
}

// TestDetectFirstRunRuntimePrefersReachableAPI proves an already-reachable API
// is the chosen shape even when binaries and compose files are also present.
func TestDetectFirstRunRuntimePrefersReachableAPI(t *testing.T) {
	probe := fakeFirstRunProbe(
		true,
		map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true},
		map[string]bool{"/ws/docker-compose.yaml": true},
	)
	detection := detectFirstRunRuntime(probe, "http://localhost:8080", "/ws")
	if detection.Shape != firstRunShapeExistingAPI {
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
	detection := detectFirstRunRuntime(probe, "http://localhost:8080", "/ws")
	if detection.Shape != firstRunShapeLocalBinaries {
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
	detection := detectFirstRunRuntime(probe, "http://localhost:8080", "/ws")
	if detection.Shape != firstRunShapeDockerCompose {
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
	detection := detectFirstRunRuntime(probe, "http://localhost:8080", "/ws")
	if detection.Shape != firstRunShapeUnknown {
		t.Fatalf("shape = %q, want unknown", detection.Shape)
	}
}

// TestExecuteFirstRunFailsWhenRuntimeUnavailable proves first-run does not claim
// success and emits next steps when no runtime is available.
func TestExecuteFirstRunFailsWhenRuntimeUnavailable(t *testing.T) {
	deps := firstRunDeps{
		Probe:         fakeFirstRunProbe(false, map[string]bool{}, map[string]bool{}),
		FetchStatus:   func(*APIClient) (scanPipelineStatus, error) { return scanPipelineStatus{}, nil },
		ListRepos:     func(*APIClient) (repositoryListResponse, error) { return repositoryListResponse{}, nil },
		WorkspaceRoot: "/ws",
	}
	result, err := executeFirstRun(context.Background(), io.Discard, io.Discard, newFirstRunClient(), deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("executeFirstRun() error = nil, want runtime failure")
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
	deps := firstRunDeps{
		Probe:         fakeFirstRunProbe(false, map[string]bool{}, map[string]bool{"/ws/docker-compose.yaml": true}),
		FetchStatus:   func(*APIClient) (scanPipelineStatus, error) { return scanPipelineStatus{}, nil },
		ListRepos:     func(*APIClient) (repositoryListResponse, error) { return repositoryListResponse{}, nil },
		WorkspaceRoot: "/ws",
	}
	result, err := executeFirstRun(context.Background(), io.Discard, io.Discard, newFirstRunClient(), deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("executeFirstRun() error = nil, want compose-down failure")
	}
	if result.RuntimeShape != firstRunShapeDockerCompose {
		t.Fatalf("RuntimeShape = %q, want docker_compose", result.RuntimeShape)
	}
	if !strings.Contains(err.Error(), "docker compose up") {
		t.Fatalf("error = %q, want compose start hint", err.Error())
	}
}

// TestExecuteFirstRunReusesExistingIndexedRepo proves the existing-API path can
// reuse an already-indexed, drained repository and answer the first query.
func TestExecuteFirstRunReusesExistingIndexedRepo(t *testing.T) {
	deps := firstRunDeps{
		Probe: fakeFirstRunProbe(true, map[string]bool{}, map[string]bool{}),
		FetchStatus: func(*APIClient) (scanPipelineStatus, error) {
			return scanPipelineStatus{
				Health:            scanHealth{State: "healthy"},
				GenerationHistory: scanGenerationHistory{Completed: 1},
			}, nil
		},
		ListRepos: func(*APIClient) (repositoryListResponse, error) {
			return repositoryListResponse{Repositories: []repositorySelectorEntry{{ID: "r1", Name: "demo", LocalPath: "/ws"}}}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, *APIClient, scanOptions, bool) (scanResult, error) {
			t.Fatal("RunScan should not be called when a complete index already exists")
			return scanResult{}, nil
		},
		WorkspaceRoot: "/ws",
	}
	result, err := executeFirstRun(context.Background(), io.Discard, io.Discard, newFirstRunClient(), deps, baseFirstRunOptions())
	if err != nil {
		t.Fatalf("executeFirstRun() error = %v, want nil", err)
	}
	if !result.succeeded() {
		t.Fatal("result.succeeded() = false, want true")
	}
	if result.RepoIndexed != "complete" {
		t.Fatalf("RepoIndexed = %q, want complete", result.RepoIndexed)
	}
}

// TestExecuteFirstRunRunsScanThenQuery proves the local path runs a scan and
// then a bounded query, reporting success only after the query returns.
func TestExecuteFirstRunRunsScanThenQuery(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())
	var scanCalled bool
	deps := firstRunDeps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(*APIClient) (scanPipelineStatus, error) {
			// No repositories yet, so detection returns no existing index.
			return scanPipelineStatus{Health: scanHealth{State: "progressing"}}, nil
		},
		ListRepos: func(*APIClient) (repositoryListResponse, error) {
			if !scanCalled {
				return repositoryListResponse{}, nil
			}
			return repositoryListResponse{Repositories: []repositorySelectorEntry{{ID: "r1", Name: "demo"}}}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, *APIClient, scanOptions, bool) (scanResult, error) {
			scanCalled = true
			return scanResult{Status: "ready"}, nil
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := executeFirstRun(context.Background(), io.Discard, io.Discard, newFirstRunClient(), deps, baseFirstRunOptions())
	if err != nil {
		t.Fatalf("executeFirstRun() error = %v, want nil", err)
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
	t.Setenv("ESHU_HOME", t.TempDir())
	deps := firstRunDeps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(*APIClient) (scanPipelineStatus, error) {
			return scanPipelineStatus{Health: scanHealth{State: "progressing"}}, nil
		},
		ListRepos: func(*APIClient) (repositoryListResponse, error) {
			return repositoryListResponse{}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, *APIClient, scanOptions, bool) (scanResult, error) {
			return scanResult{
					Status:       "failed",
					StatusReport: scanPipelineStatus{Queue: scanQueue{DeadLetter: 2}, Health: scanHealth{State: "degraded"}},
				},
				errors.New("scan readiness timed out: queue has dead-letter work")
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := executeFirstRun(context.Background(), io.Discard, io.Discard, newFirstRunClient(), deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("executeFirstRun() error = nil, want dead-letter failure")
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
	t.Setenv("ESHU_HOME", t.TempDir())
	deps := firstRunDeps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(*APIClient) (scanPipelineStatus, error) {
			return scanPipelineStatus{Health: scanHealth{State: "progressing"}}, nil
		},
		ListRepos: func(*APIClient) (repositoryListResponse, error) { return repositoryListResponse{}, nil },
		RunScan: func(context.Context, io.Writer, io.Writer, *APIClient, scanOptions, bool) (scanResult, error) {
			return scanResult{Status: "partial", StatusReport: scanPipelineStatus{Health: scanHealth{State: "degraded"}}},
				errors.New("scan readiness timed out: queue still has outstanding work")
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := executeFirstRun(context.Background(), io.Discard, io.Discard, newFirstRunClient(), deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("executeFirstRun() error = nil, want partial-readiness failure")
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
	t.Setenv("ESHU_HOME", t.TempDir())
	var calls int
	deps := firstRunDeps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(*APIClient) (scanPipelineStatus, error) {
			return scanPipelineStatus{Health: scanHealth{State: "progressing"}}, nil
		},
		ListRepos: func(*APIClient) (repositoryListResponse, error) {
			calls++
			// First call is the existing-index probe (empty), final query errors.
			if calls == 1 {
				return repositoryListResponse{}, nil
			}
			return repositoryListResponse{}, errors.New("connection refused")
		},
		RunScan: func(context.Context, io.Writer, io.Writer, *APIClient, scanOptions, bool) (scanResult, error) {
			return scanResult{Status: "ready"}, nil
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := executeFirstRun(context.Background(), io.Discard, io.Discard, newFirstRunClient(), deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("executeFirstRun() error = nil, want query failure")
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
	deps := firstRunDeps{
		ListRepos: func(*APIClient) (repositoryListResponse, error) { return repositoryListResponse{}, nil },
	}
	answer, err := runFirstRunQuery(deps, newFirstRunClient())
	if err != nil {
		t.Fatalf("runFirstRunQuery() error = %v, want nil", err)
	}
	if !strings.Contains(answer, "0 repositories") {
		t.Fatalf("answer = %q, want 0 repositories", answer)
	}
}

// TestFinishFirstRunJSONEnvelope proves the JSON envelope carries data, truth,
// and a non-nil error on failure.
func TestFinishFirstRunJSONEnvelope(t *testing.T) {
	cmd := newTestFirstRunCommand(t)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}
	opts := baseFirstRunOptions()
	opts.JSON = true
	result := newFirstRunResult("http://localhost:8080")
	result.RuntimeShape = firstRunShapeUnknown
	runErr := errors.New("verify runtime: no runtime")

	err := finishFirstRun(cmd, opts, result, runErr)
	if err == nil {
		t.Fatal("finishFirstRun() error = nil, want propagated runErr")
	}
	var payload map[string]any
	if jsonErr := json.Unmarshal(out.Bytes(), &payload); jsonErr != nil {
		t.Fatalf("json.Unmarshal() error = %v; out=%s", jsonErr, out.String())
	}
	if payload["data"] == nil || payload["truth"] == nil {
		t.Fatalf("payload missing data/truth: %#v", payload)
	}
	errPayload, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("payload[error] = %#v, want object", payload["error"])
	}
	if msg, _ := errPayload["message"].(string); !strings.Contains(msg, "verify runtime") {
		t.Fatalf("error message = %q, want verify runtime detail", msg)
	}
}

// TestFirstRunCommandIsRegistered proves the command and its flags exist.
func TestFirstRunCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"first-run"})
	if err != nil {
		t.Fatalf("rootCmd.Find(first-run) error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "first-run" {
		t.Fatalf("command = %#v, want first-run", cmd)
	}
	for _, name := range []string{"json", "no-start", "timeout", "poll-interval"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("first-run flag %q missing", name)
		}
	}
}

// fakeReposDir is a ReposDir seam that avoids touching the real filesystem
// layout so orchestration tests stay hermetic.
func fakeReposDir(root string) (string, error) {
	return root + "/.cache/repos", nil
}

func newTestFirstRunCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addFirstRunFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}
