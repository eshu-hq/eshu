// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

const fakeBearerToken = "eshu_live_SUPERSECRET_abc123def456ghi789"

// renderForTest renders the platform snippet block for a request, failing the
// test on error.
func renderForTest(t *testing.T, platform string, req mcpSetupRequest) string {
	t.Helper()
	p, err := resolvePlatform(platform)
	if err != nil {
		t.Fatalf("resolvePlatform(%q) error = %v, want nil", platform, err)
	}
	out, err := renderSetupSnippet(p, req)
	if err != nil {
		t.Fatalf("renderSetupSnippet(%q) error = %v, want nil", platform, err)
	}
	return out
}

func TestSnippetCodexLocalStdio(t *testing.T) {
	t.Parallel()
	out := renderForTest(t, "codex", mcpSetupRequest{Mode: modeLocalStdio})
	if !strings.Contains(out, "[mcp_servers.eshu]") {
		t.Fatalf("codex snippet missing [mcp_servers.eshu] block:\n%s", out)
	}
	if !strings.Contains(out, `command = "eshu"`) {
		t.Fatalf("codex snippet missing command:\n%s", out)
	}
	if !strings.Contains(out, "config.toml") {
		t.Fatalf("codex snippet missing target file hint:\n%s", out)
	}
}

func TestSnippetClaudeLocalStdio(t *testing.T) {
	t.Parallel()
	out := renderForTest(t, "claude", mcpSetupRequest{Mode: modeLocalStdio})
	// Snippet must be valid JSON with an mcpServers.eshu stdio entry.
	jsonPart := extractJSON(t, out)
	var doc map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &doc); err != nil {
		t.Fatalf("claude snippet is not valid JSON: %v\n%s", err, jsonPart)
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("claude snippet missing mcpServers object:\n%s", jsonPart)
	}
	if _, ok := servers["eshu"].(map[string]any); !ok {
		t.Fatalf("claude snippet missing eshu server entry:\n%s", jsonPart)
	}
}

func TestSnippetGenericJSON(t *testing.T) {
	t.Parallel()
	out := renderForTest(t, "generic", mcpSetupRequest{Mode: modeLocalStdio})
	jsonPart := extractJSON(t, out)
	var doc map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &doc); err != nil {
		t.Fatalf("generic snippet is not valid JSON: %v\n%s", err, jsonPart)
	}
	if _, ok := doc["mcpServers"].(map[string]any); !ok {
		t.Fatalf("generic snippet missing mcpServers:\n%s", jsonPart)
	}
}

func TestSnippetVSCodeUsesServersKey(t *testing.T) {
	t.Parallel()
	out := renderForTest(t, "vscode", mcpSetupRequest{Mode: modeLocalStdio})
	jsonPart := extractJSON(t, out)
	var doc map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &doc); err != nil {
		t.Fatalf("vscode snippet is not valid JSON: %v\n%s", err, jsonPart)
	}
	if _, ok := doc["servers"].(map[string]any); !ok {
		t.Fatalf("vscode snippet missing servers key:\n%s", jsonPart)
	}
}

func TestUnsupportedPlatformError(t *testing.T) {
	t.Parallel()
	_, err := resolvePlatform("emacs")
	if err == nil {
		t.Fatal("resolvePlatform(emacs) error = nil, want non-nil")
	}
	for _, want := range []string{"codex", "claude", "cursor", "vscode", "generic"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not list supported platform %q", err.Error(), want)
		}
	}
}

func TestHostedTokenNeverLeaksRaw(t *testing.T) {
	t.Parallel()
	// Every platform that supports env-var refs must emit the reference, and no
	// platform may print the raw token value.
	for _, platform := range []string{"codex", "claude", "cursor", "vscode", "generic"} {
		req := mcpSetupRequest{
			Mode:       modeHostedHTTP,
			ServiceURL: "https://eshu.example.com",
			APIKey:     fakeBearerToken,
		}
		out := renderForTest(t, platform, req)
		if strings.Contains(out, fakeBearerToken) {
			t.Fatalf("platform %q leaked raw token in output:\n%s", platform, out)
		}
		if !strings.Contains(out, "${"+apiKeyEnvVar+"}") {
			t.Fatalf("platform %q did not emit env-var token reference:\n%s", platform, out)
		}
	}
}

func TestRedactTokenNeverReturnsRaw(t *testing.T) {
	t.Parallel()
	got := redactToken(fakeBearerToken)
	if got == fakeBearerToken {
		t.Fatal("redactToken returned the raw token")
	}
	if strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("redactToken leaked secret body: %q", got)
	}
	if !strings.HasPrefix(got, "eshu") {
		t.Fatalf("redactToken should keep a short recognizable prefix, got %q", got)
	}
	if redactToken("") != "" {
		t.Fatal("redactToken(\"\") should be empty")
	}
	if got := redactToken("short"); strings.Contains(got, "short") {
		t.Fatalf("short token not fully masked: %q", got)
	}
}

func TestMergePreservesExistingConfig(t *testing.T) {
	t.Parallel()
	existing := []byte(`{
  "mcpServers": {
    "other": {"command": "other-bin", "args": ["x"]},
    "eshu": {"command": "stale"}
  },
  "unrelatedTopKey": {"keep": true}
}`)
	entry := localStdioServerEntry()
	merged, err := mergeMCPServerConfig(existing, "mcpServers", entry)
	if err != nil {
		t.Fatalf("mergeMCPServerConfig error = %v, want nil", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatalf("merged config not valid JSON: %v\n%s", err, merged)
	}
	// Unrelated top-level key preserved.
	if _, ok := doc["unrelatedTopKey"].(map[string]any); !ok {
		t.Fatalf("merge dropped unrelatedTopKey:\n%s", merged)
	}
	servers := doc["mcpServers"].(map[string]any)
	// Other server preserved.
	if _, ok := servers["other"].(map[string]any); !ok {
		t.Fatalf("merge dropped 'other' server:\n%s", merged)
	}
	// eshu entry replaced with the fresh stdio entry.
	eshu := servers["eshu"].(map[string]any)
	if eshu["command"] != "eshu" {
		t.Fatalf("eshu entry not refreshed, got %#v", eshu)
	}
}

func TestMergeEmptyExisting(t *testing.T) {
	t.Parallel()
	merged, err := mergeMCPServerConfig(nil, "mcpServers", localStdioServerEntry())
	if err != nil {
		t.Fatalf("mergeMCPServerConfig(nil) error = %v, want nil", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatalf("merged config not valid JSON: %v", err)
	}
	if _, ok := doc["mcpServers"].(map[string]any); !ok {
		t.Fatalf("merge of empty config missing mcpServers:\n%s", merged)
	}
}

func TestMergeRejectsMalformedExisting(t *testing.T) {
	t.Parallel()
	_, err := mergeMCPServerConfig([]byte("{not json"), "mcpServers", localStdioServerEntry())
	if err == nil {
		t.Fatal("mergeMCPServerConfig(malformed) error = nil, want non-nil to avoid clobber")
	}
}

func TestWriteMCPServerConfigMergesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"other":{"command":"keep"}}}`), 0o644); err != nil {
		t.Fatalf("seed write error = %v", err)
	}
	p, _ := resolvePlatform("cursor")
	if err := writeMCPServerConfig(p, mcpSetupRequest{Mode: modeLocalStdio}, path); err != nil {
		t.Fatalf("writeMCPServerConfig error = %v, want nil", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back error = %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("written config not valid JSON: %v\n%s", err, data)
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"].(map[string]any); !ok {
		t.Fatalf("write dropped existing 'other' server:\n%s", data)
	}
	if _, ok := servers["eshu"].(map[string]any); !ok {
		t.Fatalf("write missing eshu server:\n%s", data)
	}
}

func TestWriteRefusesNonWritablePlatform(t *testing.T) {
	t.Parallel()
	p, _ := resolvePlatform("codex")
	err := writeMCPServerConfig(p, mcpSetupRequest{Mode: modeLocalStdio}, filepath.Join(t.TempDir(), "x.json"))
	if err == nil {
		t.Fatal("writeMCPServerConfig(codex) error = nil, want non-nil")
	}
}

// extractJSON returns the first balanced JSON object in a rendered snippet
// block, starting at the first '{' and ending at its matching '}'. It is
// brace-balance aware so trailing note text containing '}' does not confuse it.
func extractJSON(t *testing.T, s string) string {
	t.Helper()
	start := strings.Index(s, "{")
	if start < 0 {
		t.Fatalf("no JSON object found in:\n%s", s)
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	t.Fatalf("unbalanced JSON object in:\n%s", s)
	return ""
}

// failingHealth is a healthProber that always reports unreachable.
type failingHealth struct{}

func (failingHealth) Reachable() error { return errFakeUnreachable }

// okHealth is a healthProber that always reports reachable.
type okHealth struct{}

func (okHealth) Reachable() error { return nil }

// okQuery is a queryProber that always succeeds.
type okQuery struct{}

func (okQuery) Smoke() error { return nil }

// failQuery is a queryProber that always fails.
type failQuery struct{}

func (failQuery) Smoke() error { return errFakeQuery }

var (
	errFakeUnreachable = &fakeErr{"unreachable"}
	errFakeQuery       = &fakeErr{"query failed"}
)

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }

func TestVerificationLocalStdioSkipsEndpointStages(t *testing.T) {
	t.Parallel()
	report := runVerification("snippet", mcp.ReadOnlyTools, nil, nil)
	byStage := stageMap(report)
	if !byStage[stageConfigGenerated].OK {
		t.Fatal("config generated stage should pass")
	}
	if !byStage[stageToolsVisible].OK {
		t.Fatal("tools visible stage should pass with embedded tools")
	}
	if !byStage[stageClientReachable].Skipped {
		t.Fatal("client reachable should be skipped for local stdio")
	}
	if !byStage[stageFirstQuery].Skipped {
		t.Fatal("first query should be skipped for local stdio")
	}
	if !report.allOK() {
		t.Fatal("local stdio verification should be all-OK (skipped stages do not fail)")
	}
}

func TestVerificationHostedAllStages(t *testing.T) {
	t.Parallel()
	report := runVerification("snippet", mcp.ReadOnlyTools, okHealth{}, okQuery{})
	if !report.allOK() {
		t.Fatalf("hosted verification should pass, got %+v", report.Stages)
	}
}

func TestVerificationUnreachableFails(t *testing.T) {
	t.Parallel()
	report := runVerification("snippet", mcp.ReadOnlyTools, failingHealth{}, okQuery{})
	byStage := stageMap(report)
	if byStage[stageClientReachable].OK {
		t.Fatal("reachable stage should fail when health probe errors")
	}
	if report.allOK() {
		t.Fatal("report should not be all-OK when a stage fails")
	}
}

func TestVerificationHealthIsNotQuerySuccess(t *testing.T) {
	t.Parallel()
	// Reachable but the first query fails: report must distinguish the two.
	report := runVerification("snippet", mcp.ReadOnlyTools, okHealth{}, failQuery{})
	byStage := stageMap(report)
	if !byStage[stageClientReachable].OK {
		t.Fatal("reachable stage should pass")
	}
	if byStage[stageFirstQuery].OK {
		t.Fatal("first query stage must fail independently of reachability")
	}
	if report.allOK() {
		t.Fatal("report should fail when first query fails even though reachable")
	}
}

func TestVerificationEmptySnippetFails(t *testing.T) {
	t.Parallel()
	report := runVerification("", mcp.ReadOnlyTools, nil, nil)
	byStage := stageMap(report)
	if byStage[stageConfigGenerated].OK {
		t.Fatal("config generated stage must fail when snippet is empty")
	}
	if report.allOK() {
		t.Fatal("report should fail when config generation fails")
	}
}

func stageMap(report verifyReport) map[verifyStage]stageResult {
	m := make(map[verifyStage]stageResult, len(report.Stages))
	for _, s := range report.Stages {
		m[s.Stage] = s
	}
	return m
}
