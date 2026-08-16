// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
)

// successFirstRunResult builds a fully-successful first-run result through the
// firstrun package's exported surface, used as the happy-path envelope fixture
// for the report subcommand.
func successFirstRunResult() firstrun.Result {
	r := firstrun.NewResult("http://localhost:8080")
	r.RuntimeShape = firstrun.ShapeExistingAPI
	r.RepoTarget = "/work/eshu"
	r.RepoIndexed = "complete"
	r.Readiness = "ready"
	r.QueryAnswered = true
	r.QuerySummary = "repositories query returned 3 (e.g. eshu)"
	r.Steps = []firstrun.Step{
		{Name: "detect runtime", Status: firstrun.StepOK, Detail: "reachable API"},
		{Name: "verify runtime", Status: firstrun.StepOK},
		{Name: "index repository", Status: firstrun.StepOK, Detail: "reused existing indexed repository"},
		{Name: "wait for readiness", Status: firstrun.StepOK, Detail: "ready"},
		{Name: "first query", Status: firstrun.StepOK, Detail: r.QuerySummary},
	}
	r.Truth = firstrun.Truth(r, "")
	return r
}

// marshalFirstRunEnvelope wraps a result in the canonical `{data, truth, error}`
// envelope `eshu first-run --json` emits.
func marshalFirstRunEnvelope(t *testing.T, result firstrun.Result) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"data":  result,
		"truth": result.Truth,
		"error": nil,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return raw
}

// runFirstRunReportCommand executes `first-run report` against the envelope on
// stdin with the given format and returns stdout.
func runFirstRunReportCommand(t *testing.T, envelope []byte, format string) string {
	t.Helper()
	cmd := newFirstRunReportCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewReader(envelope))
	if err := cmd.Flags().Set("format", format); err != nil {
		t.Fatalf("Set(format=%s): %v", format, err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(format=%s): %v", format, err)
	}
	return out.String()
}

// TestFirstRunReportSubcommandRendersEnvelope proves `first-run report` rebuilds
// the evidence report from a saved --json envelope and redacts secrets, without
// re-running any step.
func TestFirstRunReportSubcommandRendersEnvelope(t *testing.T) {
	const secret = "envelopesecrettoken1234567890"
	result := successFirstRunResult()
	result.ServiceURL = "https://user:" + secret + "@hosted.example.com/api"

	out := runFirstRunReportCommand(t, marshalFirstRunEnvelope(t, result), "json")
	if strings.Contains(out, secret) {
		t.Fatal("report subcommand output leaks the embedded credential")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("unmarshal rendered json: %v; out=%s", err, out)
	}
	if decoded["indexing_state"] != "complete" {
		t.Fatalf("indexing_state = %v, want complete", decoded["indexing_state"])
	}
}

// TestFirstRunReportSubcommandRejectsEmptyEnvelope proves a non-envelope input
// is rejected rather than silently producing an empty report.
func TestFirstRunReportSubcommandRejectsEmptyEnvelope(t *testing.T) {
	cmd := newFirstRunReportCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(`{"truth":{}}`))
	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want missing data block error")
	}
}

// TestFirstRunReportSubcommandRegistered proves the report subcommand is wired
// under first-run with its flags.
func TestFirstRunReportSubcommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"first-run", "report"})
	if err != nil {
		t.Fatalf("rootCmd.Find(first-run report) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "report" {
		t.Fatalf("command = %#v, want report", cmd)
	}
	for _, name := range []string{"from", "format", "out"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("report flag %q missing", name)
		}
	}
}

// TestFirstRunReportFlagsRegistered proves the evidence flags exist on first-run.
func TestFirstRunReportFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"first-run"})
	if err != nil {
		t.Fatalf("rootCmd.Find(first-run) error = %v", err)
	}
	for _, name := range []string{"report", "report-format", "report-out"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("first-run flag %q missing", name)
		}
	}
}

// TestFirstRunReportReEmitRedactsComposedStrings proves the latent case: a saved
// `eshu first-run --json` envelope re-rendered later by `eshu first-run report`
// must not reconstitute a credential from the composed strings it stored. This
// is the worst-reachability surface because the credential sits in an artifact
// on disk and is re-emitted on demand, long after the failing run.
//
// The envelope is hand-built with raw sentinel-bearing strings rather than
// composed through internal/cli/firstrun's production helpers, because that is
// exactly what this surface receives: operator-supplied JSON whose provenance
// the report cannot trust. The composition-path proof lives beside those
// helpers in internal/cli/firstrun's own composed-redaction tests.
func TestFirstRunReportReEmitRedactsComposedStrings(t *testing.T) {
	const (
		apiSentinel    = "LEAKSENTINEL-1"
		mcpSentinel    = "LEAKSENTINEL-2"
		targetSentinel = "LEAKSENTINEL-3"
	)
	apiBase := "http://svcuser:" + apiSentinel + "@127.0.0.1:59413"
	mcpEndpoint := "http://mcpuser:" + mcpSentinel + "@127.0.0.1:59413/api/v0"
	repoTarget := "/home/" + targetSentinel + "/work/repo"

	result := firstrun.NewResult(apiBase)
	result.RuntimeShape = firstrun.ShapeDockerCompose
	result.RepoTarget = repoTarget
	result.RepoIndexed = "failed"
	result.Readiness = "API not reachable at " + apiBase + " while probing"
	result.Steps = []firstrun.Step{
		{Name: "verify runtime", Status: firstrun.StepFailed, Detail: "API not reachable at " + apiBase},
	}
	result.NextSteps = []string{"Ask a deeper question: eshu story " + repoTarget}
	result.Diagnostic = &firstrun.Diagnostic{
		Class:         firstrun.ClassMCPEndpointIsAPI,
		Summary:       "MCP endpoint points at the API instead of the MCP service: " + mcpEndpoint,
		RecoverySteps: []string{"Re-run setup: eshu mcp setup"},
		DocsLink:      "docs/public/guides/mcp-guide.md",
		Underlying:    errors.New("verify runtime: API not reachable at " + apiBase),
	}
	result.Truth = map[string]any{"freshness": "stale", "completeness": "partial"}

	envelope := marshalFirstRunEnvelope(t, result)
	for _, format := range []string{"md", "json"} {
		out := runFirstRunReportCommand(t, envelope, format)
		for _, sentinel := range []string{apiSentinel, mcpSentinel, targetSentinel} {
			if strings.Contains(out, sentinel) {
				t.Errorf("first-run report re-emit (%s) leaks %s through a composed string", format, sentinel)
			}
		}
	}
}

// TestFirstRunReportReEmitScrubsNestedTruthValues proves the truth metadata an
// operator-supplied envelope carries is scrubbed at every depth by the real
// `first-run report` command, since the command decodes it into map[string]any
// and re-emits whatever nesting the JSON carried.
func TestFirstRunReportReEmitScrubsNestedTruthValues(t *testing.T) {
	const (
		nestedSentinel = "LEAKSENTINEL-5"
		arraySentinel  = "LEAKSENTINEL-6"
		deepSentinel   = "LEAKSENTINEL-7"
	)
	endpoint := func(sentinel string) string {
		return "http://svcuser:" + sentinel + "@127.0.0.1:59413/x"
	}

	result := successFirstRunResult()
	result.Truth = map[string]any{
		"level":  "exact",
		"source": map[string]any{"endpoint": endpoint(nestedSentinel)},
		"notes":  []any{"answered from " + endpoint(arraySentinel)},
		"probes": []any{map[string]any{"target": endpoint(deepSentinel)}},
	}

	envelope := marshalFirstRunEnvelope(t, result)
	for _, format := range []string{"md", "json"} {
		out := runFirstRunReportCommand(t, envelope, format)
		for _, sentinel := range []string{nestedSentinel, arraySentinel, deepSentinel} {
			if strings.Contains(out, sentinel) {
				t.Errorf("first-run report re-emit (%s) leaks %s from nested truth metadata", format, sentinel)
			}
		}
	}
}
