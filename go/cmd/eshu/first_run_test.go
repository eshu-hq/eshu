// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
)

func baseFirstRunOptions() firstrun.Options {
	return firstrun.Options{
		Path:         ".",
		Timeout:      time.Minute,
		PollInterval: time.Millisecond,
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
	result := firstrun.NewResult("http://localhost:8080")
	result.RuntimeShape = firstrun.ShapeUnknown
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

// TestFirstRunDepsWireProductionSeams proves runFirstRun's seam set is fully
// wired: every Deps field carries a production function, and ResolveMCPEndpoint
// specifically resolves through the wrapper's config seam. Before this test,
// the config read was pinned only in isolation and the composed-redaction
// fixture stubbed the seam, so dropping the ResolveMCPEndpoint assignment
// would have silently disabled the mcp_endpoint_is_api diagnostic with no red
// test (#6153 review).
func TestFirstRunDepsWireProductionSeams(t *testing.T) {
	home := t.TempDir()
	t.Setenv(appHomeEnvVar, home)
	t.Setenv("ESHU_MCP_URL", "")
	t.Setenv("ESHU_MCP_ENDPOINT", "")

	deps := firstRunDeps(NewAPIClient("http://localhost:8080", "", ""))

	if deps.Probe.APIHealthy == nil || deps.Probe.LookPath == nil || deps.Probe.FileExists == nil {
		t.Fatalf("probe seams not fully wired: %+v", deps.Probe)
	}
	if deps.FetchStatus == nil || deps.ListRepos == nil || deps.RunScan == nil || deps.ReposDir == nil {
		t.Fatal("pipeline seams not fully wired")
	}
	if deps.ScanRuntime.Client == nil || deps.ScanRuntime.LookPath == nil {
		t.Fatalf("scan runtime seams not wired: %+v", deps.ScanRuntime)
	}
	if deps.MatchesSelector == nil {
		t.Fatal("selector seam not wired")
	}
	if deps.ResolveMCPEndpoint == nil {
		t.Fatal("ResolveMCPEndpoint seam not wired")
	}
	if err := os.WriteFile(filepath.Join(home, envFileName), []byte("ESHU_MCP_URL=http://primary:8081/mcp/message\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if got := deps.ResolveMCPEndpoint(); got != "http://primary:8081/mcp/message" {
		t.Fatalf("deps.ResolveMCPEndpoint() = %q, want the config-backed endpoint; the seam is not the production resolver", got)
	}
}

// TestFirstRunJSONEnvelopeRoundTripsThroughParseEnvelope decodes the JSON the
// production emitter actually writes with the one canonical decode
// (firstrun.ParseEnvelope) that the benchmark, evidence report, and demo
// consumers share. The emitter builds its envelope from hand-written string
// keys, so a renamed key or a retagged Envelope field would otherwise break
// those decodes silently (#6153 review).
func TestFirstRunJSONEnvelopeRoundTripsThroughParseEnvelope(t *testing.T) {
	cmd := newTestFirstRunCommand(t)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	opts := baseFirstRunOptions()
	opts.JSON = true
	result := firstrun.NewResult("http://localhost:8080")
	result.RepoIndexed = "api-svc"
	result.Truth = map[string]any{"freshness": "fresh"}
	runErr := errors.New("verify runtime: no runtime")

	if err := finishFirstRun(cmd, opts, result, runErr); !errors.Is(err, runErr) {
		t.Fatalf("finishFirstRun() error = %v, want the propagated runErr", err)
	}

	env, parseErr := firstrun.ParseEnvelope(out.Bytes())
	if parseErr != nil {
		t.Fatalf("ParseEnvelope(emitter output) error = %v; out=%s", parseErr, out.String())
	}
	if env.Data.ServiceURL != "http://localhost:8080" || env.Data.RepoIndexed != "api-svc" {
		t.Fatalf("decoded data = %+v, want the emitted result fields", env.Data)
	}
	if got, _ := env.Truth["freshness"].(string); got != "fresh" {
		t.Fatalf("decoded truth = %#v, want the emitted truth labels", env.Truth)
	}
	if env.Error == nil || env.Error.Message != "verify runtime: no runtime" {
		t.Fatalf("decoded error = %#v, want the emitted failure message", env.Error)
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

// TestResolveFirstRunMCPEndpointReadsConfigSeam pins the wrapper-owned config
// read the extracted package receives through Deps.ResolveMCPEndpoint: the
// endpoint comes from ESHU_MCP_URL in the .env file under the app home, with
// ESHU_MCP_ENDPOINT as the fallback. The composed-redaction fixture in
// internal/cli/firstrun stubs this resolver, so this test is what keeps the
// real config path proven.
func TestResolveFirstRunMCPEndpointReadsConfigSeam(t *testing.T) {
	home := t.TempDir()
	t.Setenv(appHomeEnvVar, home)
	t.Setenv("ESHU_MCP_URL", "")
	t.Setenv("ESHU_MCP_ENDPOINT", "")

	if got := resolveFirstRunMCPEndpoint(); got != "" {
		t.Fatalf("resolveFirstRunMCPEndpoint() = %q with no config, want empty", got)
	}

	if err := os.WriteFile(filepath.Join(home, envFileName), []byte("ESHU_MCP_ENDPOINT=http://fallback:9000/mcp\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if got := resolveFirstRunMCPEndpoint(); got != "http://fallback:9000/mcp" {
		t.Fatalf("resolveFirstRunMCPEndpoint() = %q, want the ESHU_MCP_ENDPOINT fallback", got)
	}

	config := "ESHU_MCP_URL=http://primary:8081/mcp/message\nESHU_MCP_ENDPOINT=http://fallback:9000/mcp\n"
	if err := os.WriteFile(filepath.Join(home, envFileName), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if got := resolveFirstRunMCPEndpoint(); got != "http://primary:8081/mcp/message" {
		t.Fatalf("resolveFirstRunMCPEndpoint() = %q, want ESHU_MCP_URL to win", got)
	}
}

// TestFirstRunSelectorMatchesAdaptsEveryField proves the adapter copies every
// field the shared selector matcher reads, so a match that keys off any of the
// five entry fields behaves exactly as it did before the extraction.
func TestFirstRunSelectorMatchesAdaptsEveryField(t *testing.T) {
	repo := firstrun.Repository{
		ID:        "id-1",
		Name:      "demo",
		Path:      "/srv/checkouts/demo",
		LocalPath: "/ws/demo",
		RepoSlug:  "team/demo",
	}
	for _, tc := range []struct {
		name     string
		selector string
		want     bool
	}{
		{"matches by name", "demo", true},
		{"matches by local path", "/ws/demo", true},
		{"matches by slug", "team/demo", true},
		{"no match", "unrelated", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstRunSelectorMatches(repo, tc.selector); got != tc.want {
				t.Fatalf("firstRunSelectorMatches(%q) = %v, want %v", tc.selector, got, tc.want)
			}
		})
	}
}

func newTestFirstRunCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addFirstRunFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}
