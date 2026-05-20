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
)

func TestRunScanRunsBootstrapAndWaitsForHealthyPipeline(t *testing.T) {
	repoPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	reportPath := filepath.Join(t.TempDir(), "reports", "discovery.json")

	reset := stubScanRuntime(t)
	defer reset()

	var gotArgs []string
	var gotEnv []string
	scanRunBootstrap = func(_ context.Context, binary string, args []string, env []string, _ io.Writer, _ io.Writer) error {
		if binary != "/bin/eshu-bootstrap-index" {
			t.Fatalf("binary = %q, want /bin/eshu-bootstrap-index", binary)
		}
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}
	statuses := []scanPipelineStatus{
		{
			Health: scanHealth{State: "healthy"},
			Queue:  scanQueue{},
		},
		{
			Health: scanHealth{State: "healthy"},
			Queue:  scanQueue{Succeeded: 12},
			GenerationHistory: scanGenerationHistory{
				Completed: 1,
			},
		},
	}
	scanFetchPipelineStatus = func(_ *APIClient) (scanPipelineStatus, error) {
		if len(statuses) == 0 {
			t.Fatal("scanFetchPipelineStatus called more times than expected")
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
}

func TestRunScanFailsOnDeadLettersByDefault(t *testing.T) {
	reset := stubScanRuntime(t)
	defer reset()

	var scanDeadLetterFetchCount atomic.Int64
	scanFetchPipelineStatus = func(_ *APIClient) (scanPipelineStatus, error) {
		if called := scanDeadLetterFetchCount.Add(1); called == 1 {
			return scanPipelineStatus{Health: scanHealth{State: "healthy"}}, nil
		}
		return scanPipelineStatus{
			Health: scanHealth{State: "degraded", Reasons: []string{"queue has dead-letter work"}},
			Queue:  scanQueue{DeadLetter: 1},
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
	scanFetchPipelineStatus = func(_ *APIClient) (scanPipelineStatus, error) {
		if called := scanJSONFetchCount.Add(1); called == 1 {
			return scanPipelineStatus{Health: scanHealth{State: "healthy"}}, nil
		}
		return scanPipelineStatus{
			Health: scanHealth{State: "healthy"},
			Queue:  scanQueue{Succeeded: 4},
			GenerationHistory: scanGenerationHistory{
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

	scanFetchPipelineStatus = func(_ *APIClient) (scanPipelineStatus, error) {
		return scanPipelineStatus{}, errors.New("connection refused")
	}
	calledBootstrap := false
	scanRunBootstrap = func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
		calledBootstrap = true
		return nil
	}

	err := runScan(newTestScanCommand(t), []string{t.TempDir()})
	if err == nil {
		t.Fatal("runScan() error = nil, want preflight failure")
	}
	if calledBootstrap {
		t.Fatal("scanRunBootstrap called after failed preflight")
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

func newTestScanCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addScanFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}

func stubScanRuntime(t *testing.T) func() {
	t.Helper()
	originalLookPath := scanLookPath
	originalRunBootstrap := scanRunBootstrap
	originalFetchStatus := scanFetchPipelineStatus
	originalFetchQueryProbe := scanFetchQueryProbe
	originalNow := scanNow
	originalSleep := scanSleep

	scanLookPath = func(file string) (string, error) {
		if file != "eshu-bootstrap-index" {
			t.Fatalf("scanLookPath(%q), want eshu-bootstrap-index", file)
		}
		return "/bin/eshu-bootstrap-index", nil
	}
	scanRunBootstrap = func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
		return nil
	}
	scanFetchPipelineStatus = func(*APIClient) (scanPipelineStatus, error) {
		return scanPipelineStatus{
			Health: scanHealth{State: "healthy"},
			Queue:  scanQueue{},
			GenerationHistory: scanGenerationHistory{
				Completed: 1,
			},
		}, nil
	}
	scanFetchQueryProbe = func(*APIClient) (map[string]any, error) {
		return map[string]any{
			"data":  map[string]any{"repositories": []any{}},
			"truth": map[string]any{"basis": "authoritative_graph"},
			"error": nil,
		}, nil
	}
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	scanNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	scanSleep = func(time.Duration) {}

	return func() {
		scanLookPath = originalLookPath
		scanRunBootstrap = originalRunBootstrap
		scanFetchPipelineStatus = originalFetchStatus
		scanFetchQueryProbe = originalFetchQueryProbe
		scanNow = originalNow
		scanSleep = originalSleep
	}
}
