// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

func TestRunScanRunsBootstrapAndWaitsForHealthyPipeline(t *testing.T) {
	repoPath := t.TempDir()
	eshuHome := t.TempDir()
	t.Setenv("ESHU_HOME", eshuHome)
	t.Setenv("ESHU_REPO_SOURCE_MODE", "githubOrg")
	t.Setenv("ESHU_FILESYSTEM_ROOT", "/should/not/leak")
	t.Setenv("ESHU_FILESYSTEM_DIRECT", "false")
	t.Setenv("ESHU_REPOS_DIR", "/should/not/leak")
	if err := os.Mkdir(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	reportPath := filepath.Join(t.TempDir(), "reports", "discovery.json")

	reset := stubScanRuntime(t)
	defer reset()

	var gotArgs []string
	var gotEnv []string
	scanStub.RunBootstrap = func(_ context.Context, binary string, args []string, env []string, _ io.Writer, _ io.Writer) error {
		if binary != "/bin/eshu-bootstrap-index" {
			t.Fatalf("binary = %q, want /bin/eshu-bootstrap-index", binary)
		}
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}
	statuses := []scan.PipelineStatus{
		{
			Health: scan.Health{State: "healthy"},
			Queue:  scan.Queue{},
		},
		{
			Health: scan.Health{State: "healthy"},
			Queue:  scan.Queue{Succeeded: 12},
			GenerationHistory: scan.GenerationHistory{
				Completed: 1,
			},
		},
	}
	scanStub.FetchStatus = func(scan.Client) (scan.PipelineStatus, error) {
		if len(statuses) == 0 {
			t.Fatal("FetchStatus called more times than expected")
		}
		next := statuses[0]
		statuses = statuses[1:]
		return next, nil
	}

	cmd := newTestScanCommand(t)
	if err := cmd.Flags().Set("discovery-report", reportPath); err != nil {
		t.Fatalf("Set(discovery-report) error = %v, want nil", err)
	}

	if err := runScan(cmd, []string{repoPath}); err != nil {
		t.Fatalf("runScan() error = %v, want nil", err)
	}

	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		t.Fatalf("Abs(repoPath) error = %v, want nil", err)
	}
	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = realPath
	}
	if got, want := strings.Join(gotArgs, " "), "eshu-bootstrap-index --path "+absPath; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	absReport, err := filepath.Abs(reportPath)
	if err != nil {
		t.Fatalf("Abs(reportPath) error = %v, want nil", err)
	}
	if !envContains(gotEnv, "ESHU_DISCOVERY_REPORT="+absReport) {
		t.Fatalf("env missing ESHU_DISCOVERY_REPORT=%q; env=%v", absReport, gotEnv)
	}
	for key, want := range map[string]string{
		"ESHU_REPO_SOURCE_MODE":  "filesystem",
		"ESHU_FILESYSTEM_ROOT":   absPath,
		"ESHU_FILESYSTEM_DIRECT": "true",
	} {
		if got := envValue(gotEnv, key); got != want {
			t.Fatalf("%s = %q, want %q; env=%v", key, got, want, gotEnv)
		}
	}
	reposDir := envValue(gotEnv, "ESHU_REPOS_DIR")
	if !strings.HasPrefix(reposDir, filepath.Join(eshuHome, "local", "workspaces")) {
		t.Fatalf("ESHU_REPOS_DIR = %q, want under ESHU_HOME workspaces %q", reposDir, eshuHome)
	}
	if !strings.HasSuffix(reposDir, filepath.Join("cache", "repos")) {
		t.Fatalf("ESHU_REPOS_DIR = %q, want cache/repos suffix", reposDir)
	}
}

func TestRunScanFailsOnDeadLettersByDefault(t *testing.T) {
	reset := stubScanRuntime(t)
	defer reset()

	var scanDeadLetterFetchCount atomic.Int64
	scanStub.FetchStatus = func(scan.Client) (scan.PipelineStatus, error) {
		if called := scanDeadLetterFetchCount.Add(1); called == 1 {
			return scan.PipelineStatus{Health: scan.Health{State: "healthy"}}, nil
		}
		return scan.PipelineStatus{
			Health: scan.Health{State: "degraded", Reasons: []string{"queue has dead-letter work"}},
			Queue:  scan.Queue{DeadLetter: 1},
		}, nil
	}

	err := runScan(newTestScanCommand(t), []string{t.TempDir()})
	if err == nil {
		t.Fatal("runScan() error = nil, want dead-letter failure")
	}
	if !strings.Contains(err.Error(), "dead-letter") {
		t.Fatalf("runScan() error = %q, want dead-letter detail", err.Error())
	}
}

func TestRunScanJSONUsesCanonicalEnvelope(t *testing.T) {
	reset := stubScanRuntime(t)
	defer reset()

	var scanJSONFetchCount atomic.Int64
	scanStub.FetchStatus = func(scan.Client) (scan.PipelineStatus, error) {
		if called := scanJSONFetchCount.Add(1); called == 1 {
			return scan.PipelineStatus{Health: scan.Health{State: "healthy"}}, nil
		}
		return scan.PipelineStatus{
			Health: scan.Health{State: "healthy"},
			Queue:  scan.Queue{Succeeded: 4},
			GenerationHistory: scan.GenerationHistory{
				Completed: 1,
			},
		}, nil
	}

	out := &bytes.Buffer{}
	cmd := newTestScanCommand(t)
	cmd.SetOut(out)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	if err := runScan(cmd, []string{t.TempDir()}); err != nil {
		t.Fatalf("runScan() error = %v, want nil", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if payload["error"] != nil {
		t.Fatalf("payload[error] = %#v, want nil", payload["error"])
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("payload[data] = %#v, want object", payload["data"])
	}
	truth, ok := payload["truth"].(map[string]any)
	if !ok {
		t.Fatalf("payload[truth] = %#v, want object", payload["truth"])
	}
	if got, want := data["status"], "ready"; got != want {
		t.Fatalf("data[status] = %#v, want %#v", got, want)
	}
	if got, want := truth["freshness"], "current"; got != want {
		t.Fatalf("truth[freshness] = %#v, want %#v", got, want)
	}
}

func TestRunScanReturnsPreflightFailureBeforeBootstrap(t *testing.T) {
	reset := stubScanRuntime(t)
	defer reset()

	scanStub.FetchStatus = func(scan.Client) (scan.PipelineStatus, error) {
		return scan.PipelineStatus{}, errors.New("connection refused")
	}
	calledBootstrap := false
	scanStub.RunBootstrap = func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
		calledBootstrap = true
		return nil
	}

	err := runScan(newTestScanCommand(t), []string{t.TempDir()})
	if err == nil {
		t.Fatal("runScan() error = nil, want preflight failure")
	}
	if calledBootstrap {
		t.Fatal("RunBootstrap called after failed preflight")
	}
}

func TestRunScanJSONReturnsEnvelopeForPreflightFailure(t *testing.T) {
	reset := stubScanRuntime(t)
	defer reset()

	scanStub.FetchStatus = func(scan.Client) (scan.PipelineStatus, error) {
		return scan.PipelineStatus{}, errors.New("connection refused")
	}
	calledBootstrap := false
	scanStub.RunBootstrap = func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
		calledBootstrap = true
		return nil
	}

	out := &bytes.Buffer{}
	cmd := newTestScanCommand(t)
	cmd.SetOut(out)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	err := runScan(cmd, []string{t.TempDir()})
	if err == nil {
		t.Fatal("runScan() error = nil, want preflight failure")
	}
	if calledBootstrap {
		t.Fatal("RunBootstrap called after failed preflight")
	}
	assertScanJSONError(t, out.Bytes(), "scan preflight status check")
}

func TestRunScanAppliesTimeoutToBootstrapContext(t *testing.T) {
	reset := stubScanRuntime(t)
	defer reset()

	var sawDeadline bool
	scanStub.RunBootstrap = func(ctx context.Context, _ string, _ []string, _ []string, _ io.Writer, _ io.Writer) error {
		_, sawDeadline = ctx.Deadline()
		return context.DeadlineExceeded
	}

	cmd := newTestScanCommand(t)
	if err := cmd.Flags().Set("timeout", "1ms"); err != nil {
		t.Fatalf("Set(timeout) error = %v, want nil", err)
	}

	err := runScan(cmd, []string{t.TempDir()})
	if err == nil {
		t.Fatal("runScan() error = nil, want bootstrap deadline failure")
	}
	if !sawDeadline {
		t.Fatal("bootstrap context has no deadline")
	}
}

func TestRunScanAllowPartialPrintsHumanWarning(t *testing.T) {
	reset := stubScanRuntime(t)
	defer reset()

	var fetchCount atomic.Int64
	scanStub.FetchStatus = func(scan.Client) (scan.PipelineStatus, error) {
		if fetchCount.Add(1) == 1 {
			return scan.PipelineStatus{Health: scan.Health{State: "healthy"}}, nil
		}
		return scan.PipelineStatus{
			Health: scan.Health{State: "degraded", Reasons: []string{"queue has dead-letter work"}},
			Queue:  scan.Queue{DeadLetter: 1},
		}, nil
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := newTestScanCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	if err := cmd.Flags().Set("allow-partial", "true"); err != nil {
		t.Fatalf("Set(allow-partial) error = %v, want nil", err)
	}

	if err := runScan(cmd, []string{t.TempDir()}); err != nil {
		t.Fatalf("runScan() error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "Scan partial") {
		t.Fatalf("stdout = %q, want Scan partial", out.String())
	}
	if !strings.Contains(errOut.String(), "Warning: queue has dead-letter work") {
		t.Fatalf("stderr = %q, want partial warning", errOut.String())
	}
}

func TestScanCommandIsRegisteredWithReadinessFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"scan"})
	if err != nil {
		t.Fatalf("rootCmd.Find(scan) error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "scan" {
		t.Fatalf("root command = %#v, want scan command", cmd)
	}
	for _, name := range []string{"wait", "timeout", "poll-interval", "allow-partial", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("scan flag %q missing", name)
		}
	}
}

// TestDefaultScanRuntimeWiresEveryRequiredSeam guards the production wiring.
// internal/cli/scan rejects a Runtime missing a process seam, so a field this
// wrapper forgot to set would otherwise only surface on a real scan.
func TestDefaultScanRuntimeWiresEveryRequiredSeam(t *testing.T) {
	rt := defaultScanRuntime(&APIClient{BaseURL: "http://localhost:8080"})

	if rt.Client == nil {
		t.Fatal("Runtime.Client = nil, want the API client")
	}
	if rt.ServiceURL != "http://localhost:8080" {
		t.Fatalf("Runtime.ServiceURL = %q, want the client base URL", rt.ServiceURL)
	}
	if len(rt.Environ) == 0 {
		t.Fatal("Runtime.Environ is empty, want the process environment")
	}
	if rt.LookPath == nil || rt.RunBootstrap == nil {
		t.Fatal("Runtime.LookPath/RunBootstrap = nil, want the process seams wired")
	}
	if rt.FetchStatus == nil || rt.FetchQueryProbe == nil {
		t.Fatal("Runtime.FetchStatus/FetchQueryProbe = nil, want the API reads wired")
	}
}

func assertScanJSONError(t *testing.T, contents []byte, want string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(contents, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, string(contents))
	}
	if payload["data"] == nil {
		t.Fatalf("payload[data] = nil, want object")
	}
	if payload["truth"] == nil {
		t.Fatalf("payload[truth] = nil, want object")
	}
	errPayload, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("payload[error] = %#v, want object", payload["error"])
	}
	if message, _ := errPayload["message"].(string); !strings.Contains(message, want) {
		t.Fatalf("error message = %q, want containing %q", message, want)
	}
}

func newTestScanCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addScanFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}

// scanStub is the fake scan runtime stubScanRuntime installs. Tests override
// individual seams on it after calling stubScanRuntime. A nil Environ resolves
// through procexec.Environ at command-run time, exactly as production does, so a
// t.Setenv after the stub still reaches the bootstrap child's environment.
var scanStub scan.Runtime

// stubScanRuntime replaces the production scan runtime factory with one that
// runs no bootstrap child, resolves no PATH, and reads no API. The returned
// function restores the factory.
func stubScanRuntime(t *testing.T) func() {
	t.Helper()
	original := scanRuntimeFor

	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	scanStub = scan.Runtime{
		LookPath: func(file string) (string, error) {
			if file != "eshu-bootstrap-index" {
				t.Fatalf("LookPath(%q), want eshu-bootstrap-index", file)
			}
			return "/bin/eshu-bootstrap-index", nil
		},
		RunBootstrap: func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
			return nil
		},
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			return scan.PipelineStatus{
				Health: scan.Health{State: "healthy"},
				Queue:  scan.Queue{},
				GenerationHistory: scan.GenerationHistory{
					Completed: 1,
				},
			}, nil
		},
		FetchQueryProbe: func(scan.Client) (map[string]any, error) {
			return map[string]any{
				"data":  map[string]any{"repositories": []any{}},
				"truth": map[string]any{"basis": "authoritative_graph"},
				"error": nil,
			}, nil
		},
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}

	scanRuntimeFor = func(client *APIClient) scan.Runtime {
		rt := scanStub
		rt.Client = client
		rt.ServiceURL = client.BaseURL
		if rt.Environ == nil {
			rt.Environ = procexec.Environ()
		}
		return rt
	}

	return func() {
		scanRuntimeFor = original
	}
}
