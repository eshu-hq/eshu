// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpsetup

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
func renderForTest(t *testing.T, platform string, req SetupRequest) string {
	t.Helper()
	p, err := ResolvePlatform(platform)
	if err != nil {
		t.Fatalf("ResolvePlatform(%q) error = %v, want nil", platform, err)
	}
	out, err := RenderSetupSnippet(p, req)
	if err != nil {
		t.Fatalf("RenderSetupSnippet(%q) error = %v, want nil", platform, err)
	}
	return out
}

func TestSnippetCodexLocalStdio(t *testing.T) {
	t.Parallel()
	out := renderForTest(t, "codex", SetupRequest{Mode: ModeLocalStdio})
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
	out := renderForTest(t, "claude", SetupRequest{Mode: ModeLocalStdio})
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
	out := renderForTest(t, "generic", SetupRequest{Mode: ModeLocalStdio})
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
	out := renderForTest(t, "vscode", SetupRequest{Mode: ModeLocalStdio})
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
	_, err := ResolvePlatform("emacs")
	if err == nil {
		t.Fatal("ResolvePlatform(emacs) error = nil, want non-nil")
	}
	for _, want := range []string{"codex", "claude", "cursor", "vscode", "generic"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not list supported platform %q", err.Error(), want)
		}
	}
}

// TestHostedTokenNeverLeaksRaw proves no platform, in any of the three auth
// postures, ever prints a raw secret -- the acceptance line from issue #5169
// (F-8): "no secret values in snippet" across token, SSO, and shared-key.
func TestHostedTokenNeverLeaksRaw(t *testing.T) {
	t.Parallel()
	const fakePersonalToken = "eshu_pat_ANOTHERSECRET_789xyz"
	postures := []struct {
		name    string
		posture AuthPosture
		apiKey  string
	}{
		{"token", PostureToken, ""},
		{"sso", PostureSSO, ""},
		{"shared-key", PostureSharedKey, fakeBearerToken},
	}
	for _, platform := range []string{"codex", "claude", "cursor", "vscode", "generic"} {
		for _, p := range postures {
			platform, p := platform, p
			t.Run(platform+"/"+p.name, func(t *testing.T) {
				t.Parallel()
				req := SetupRequest{
					Mode:       ModeHostedHTTP,
					ServiceURL: "https://eshu.example.com",
					Posture:    p.posture,
					APIKey:     p.apiKey,
					Issuers:    []string{"https://idp.example.com/oauth2/aus123"},
				}
				out := renderForTest(t, platform, req)
				if strings.Contains(out, fakeBearerToken) || strings.Contains(out, fakePersonalToken) {
					t.Fatalf("platform %q posture %q leaked a raw secret in output:\n%s", platform, p.name, out)
				}
			})
		}
	}
}

// TestHostedSnippetShapes pins the exact credential shape emitted per
// platform x posture: token references ${ESHU_MCP_TOKEN} (bearer_token_env_var
// for Codex) and never ${ESHU_API_KEY}; SSO omits any bearer credential
// entirely and names the issuer; shared-key references ${ESHU_API_KEY} and
// always carries the admin/dev warning.
func TestHostedSnippetShapes(t *testing.T) {
	t.Parallel()
	for _, platform := range []string{"codex", "claude", "cursor", "vscode", "generic"} {
		platform := platform

		t.Run(platform+"/token", func(t *testing.T) {
			t.Parallel()
			out := renderForTest(t, platform, SetupRequest{
				Mode:       ModeHostedHTTP,
				ServiceURL: "https://eshu.example.com",
				Posture:    PostureToken,
			})
			if strings.Contains(out, "${"+APIKeyEnvVar+"}") {
				t.Fatalf("%s token snippet must not reference %s:\n%s", platform, APIKeyEnvVar, out)
			}
			if platform == "codex" {
				if !strings.Contains(out, `bearer_token_env_var = "`+MCPTokenEnvVar+`"`) {
					t.Fatalf("codex token snippet missing bearer_token_env_var:\n%s", out)
				}
			} else if !strings.Contains(out, "${"+MCPTokenEnvVar+"}") {
				t.Fatalf("%s token snippet missing ${%s} reference:\n%s", platform, MCPTokenEnvVar, out)
			}
		})

		t.Run(platform+"/sso", func(t *testing.T) {
			t.Parallel()
			out := renderForTest(t, platform, SetupRequest{
				Mode:                  ModeHostedHTTP,
				ServiceURL:            "https://eshu.example.com",
				Posture:               PostureSSO,
				Issuers:               []string{"https://idp.example.com/oauth2/aus123"},
				PreregisteredClientID: "eshu-mcp-client",
			})
			if strings.Contains(out, "${"+APIKeyEnvVar+"}") || strings.Contains(out, "${"+MCPTokenEnvVar+"}") {
				t.Fatalf("%s sso snippet must not reference a bearer token env var:\n%s", platform, out)
			}
			if strings.Contains(out, "bearer_token_env_var") {
				t.Fatalf("%s sso snippet must not set bearer_token_env_var:\n%s", platform, out)
			}
			if !strings.Contains(out, "https://idp.example.com/oauth2/aus123") {
				t.Fatalf("%s sso snippet should name the issuer:\n%s", platform, out)
			}
			if !strings.Contains(out, "eshu-mcp-client") {
				t.Fatalf("%s sso snippet should name the pre-registered client id:\n%s", platform, out)
			}
			if platform != "codex" {
				jsonPart := extractJSON(t, out)
				var doc map[string]any
				if err := json.Unmarshal([]byte(jsonPart), &doc); err != nil {
					t.Fatalf("%s sso snippet not valid JSON: %v\n%s", platform, err, jsonPart)
				}
				serversKey := "mcpServers"
				if platform == "vscode" {
					serversKey = "servers"
				}
				servers, ok := doc[serversKey].(map[string]any)
				if !ok {
					t.Fatalf("%s sso snippet missing %s key:\n%s", platform, serversKey, jsonPart)
				}
				entry, ok := servers["eshu"].(map[string]any)
				if !ok {
					t.Fatalf("%s sso snippet missing eshu entry:\n%s", platform, jsonPart)
				}
				if _, present := entry["headers"]; present {
					t.Fatalf("%s sso entry must omit headers entirely:\n%s", platform, jsonPart)
				}
			}
		})

		t.Run(platform+"/shared-key", func(t *testing.T) {
			t.Parallel()
			out := renderForTest(t, platform, SetupRequest{
				Mode:       ModeHostedHTTP,
				ServiceURL: "https://eshu.example.com",
				Posture:    PostureSharedKey,
				APIKey:     fakeBearerToken,
			})
			if !strings.Contains(out, "WARNING") {
				t.Fatalf("%s shared-key snippet missing admin/dev warning:\n%s", platform, out)
			}
			if strings.Contains(out, fakeBearerToken) {
				t.Fatalf("%s shared-key snippet leaked raw token:\n%s", platform, out)
			}
			if platform == "codex" {
				if !strings.Contains(out, `bearer_token_env_var = "`+APIKeyEnvVar+`"`) {
					t.Fatalf("codex shared-key snippet missing bearer_token_env_var:\n%s", out)
				}
			} else if !strings.Contains(out, "${"+APIKeyEnvVar+"}") {
				t.Fatalf("%s shared-key snippet missing ${%s} reference:\n%s", platform, APIKeyEnvVar, out)
			}
		})
	}
}

func TestRedactTokenNeverReturnsRaw(t *testing.T) {
	t.Parallel()
	got := RedactToken(fakeBearerToken)
	if got == fakeBearerToken {
		t.Fatal("RedactToken returned the raw token")
	}
	if strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("RedactToken leaked secret body: %q", got)
	}
	if !strings.HasPrefix(got, "eshu") {
		t.Fatalf("RedactToken should keep a short recognizable prefix, got %q", got)
	}
	if RedactToken("") != "" {
		t.Fatal("RedactToken(\"\") should be empty")
	}
	if got := RedactToken("short"); strings.Contains(got, "short") {
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
	p, _ := ResolvePlatform("cursor")
	if err := WriteMCPServerConfig(p, SetupRequest{Mode: ModeLocalStdio}, path); err != nil {
		t.Fatalf("WriteMCPServerConfig error = %v, want nil", err)
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
	p, _ := ResolvePlatform("codex")
	err := WriteMCPServerConfig(p, SetupRequest{Mode: ModeLocalStdio}, filepath.Join(t.TempDir(), "x.json"))
	if err == nil {
		t.Fatal("WriteMCPServerConfig(codex) error = nil, want non-nil")
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

// failingHealth is a HealthProber that always reports unreachable.
type failingHealth struct{}

func (failingHealth) Reachable() error { return errFakeUnreachable }

// okHealth is a HealthProber that always reports reachable.
type okHealth struct{}

func (okHealth) Reachable() error { return nil }

// okQuery is a QueryProber that always succeeds.
type okQuery struct{}

func (okQuery) Smoke() error { return nil }

// failQuery is a QueryProber that always fails.
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
	report := RunVerification("snippet", mcp.ReadOnlyTools, nil, nil, "")
	byStage := stageMap(report)
	if !byStage[StageConfigGenerated].OK {
		t.Fatal("config generated stage should pass")
	}
	if !byStage[StageToolsVisible].OK {
		t.Fatal("tools visible stage should pass with embedded tools")
	}
	if !byStage[StageClientReachable].Skipped {
		t.Fatal("client reachable should be skipped for local stdio")
	}
	if !byStage[StageFirstQuery].Skipped {
		t.Fatal("first query should be skipped for local stdio")
	}
	if !report.AllOK() {
		t.Fatal("local stdio verification should be all-OK (skipped stages do not fail)")
	}
}

func TestVerificationHostedAllStages(t *testing.T) {
	t.Parallel()
	report := RunVerification("snippet", mcp.ReadOnlyTools, okHealth{}, okQuery{}, "")
	if !report.AllOK() {
		t.Fatalf("hosted verification should pass, got %+v", report.Stages)
	}
}

func TestVerificationUnreachableFails(t *testing.T) {
	t.Parallel()
	report := RunVerification("snippet", mcp.ReadOnlyTools, failingHealth{}, okQuery{}, "")
	byStage := stageMap(report)
	if byStage[StageClientReachable].OK {
		t.Fatal("reachable stage should fail when health probe errors")
	}
	if report.AllOK() {
		t.Fatal("report should not be all-OK when a stage fails")
	}
}

func TestVerificationHealthIsNotQuerySuccess(t *testing.T) {
	t.Parallel()
	// Reachable but the first query fails: report must distinguish the two.
	report := RunVerification("snippet", mcp.ReadOnlyTools, okHealth{}, failQuery{}, "")
	byStage := stageMap(report)
	if !byStage[StageClientReachable].OK {
		t.Fatal("reachable stage should pass")
	}
	if byStage[StageFirstQuery].OK {
		t.Fatal("first query stage must fail independently of reachability")
	}
	if report.AllOK() {
		t.Fatal("report should fail when first query fails even though reachable")
	}
}

func TestVerificationEmptySnippetFails(t *testing.T) {
	t.Parallel()
	report := RunVerification("", mcp.ReadOnlyTools, nil, nil, "")
	byStage := stageMap(report)
	if byStage[StageConfigGenerated].OK {
		t.Fatal("config generated stage must fail when snippet is empty")
	}
	if report.AllOK() {
		t.Fatal("report should fail when config generation fails")
	}
}

func stageMap(report VerifyReport) map[VerifyStage]StageResult {
	m := make(map[VerifyStage]StageResult, len(report.Stages))
	for _, s := range report.Stages {
		m[s.Stage] = s
	}
	return m
}
