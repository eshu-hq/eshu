// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpsetup

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
)

var shapeTestPostures = map[string]AuthPosture{
	"token": PostureToken, "sso": PostureSSO, "shared-key": PostureSharedKey,
}

// TestOnlyClaudeAndCodexAreOptedIntoTheStructuredShape pins the platform
// profile: the two clients measured to pay for the second copy get
// structured; every other platform stays on dual.
func TestOnlyClaudeAndCodexAreOptedIntoTheStructuredShape(t *testing.T) {
	t.Parallel()
	var structured []string
	for _, name := range SupportedPlatformNames() {
		p, err := ResolvePlatform(name)
		if err != nil {
			t.Fatalf("ResolvePlatform(%q): %v", name, err)
		}
		if resultshape.OrDual(p.ResultShape) == resultshape.Structured {
			structured = append(structured, name)
		}
	}
	sort.Strings(structured)
	if got := strings.Join(structured, ","); got != "claude,codex" {
		t.Fatalf("structured platforms = %q, want claude,codex", got)
	}
}

func TestHostedSnippetCarriesTheShapeOnlyForOptedInPlatforms(t *testing.T) {
	t.Parallel()
	const bare = "https://your-eshu-host/mcp/message"
	for _, name := range SupportedPlatformNames() {
		for postureName, posture := range shapeTestPostures {
			t.Run(name+"/"+postureName, func(t *testing.T) {
				t.Parallel()
				out := renderForTest(t, name, SetupRequest{Mode: ModeHostedHTTP, ServiceURL: "https://your-eshu-host", Posture: posture})
				optedIn := name == "claude" || name == "codex"
				if got := strings.Contains(out, bare+"?shape=structured"); got != optedIn {
					t.Fatalf("%s hosted snippet has ?shape=structured = %v, want %v:\n%s", name, got, optedIn, out)
				}
				if !optedIn && (strings.Contains(out, "shape") || !strings.Contains(out, bare)) {
					t.Fatalf("%s hosted snippet must carry the bare endpoint and no shape:\n%s", name, out)
				}
			})
		}
	}
}

func TestLocalStdioSnippetCarriesTheShapeAsEnvForOptedInPlatforms(t *testing.T) {
	t.Parallel()
	for _, name := range SupportedPlatformNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := renderForTest(t, name, SetupRequest{Mode: ModeLocalStdio})
			switch name {
			case "claude":
				var doc struct {
					MCPServers map[string]struct {
						Env map[string]string `json:"env"`
					} `json:"mcpServers"`
				}
				if err := json.Unmarshal([]byte(extractJSON(t, out)), &doc); err != nil {
					t.Fatalf("claude snippet is not JSON: %v\n%s", err, out)
				}
				if got := doc.MCPServers["eshu"].Env["ESHU_MCP_RESULT_SHAPE"]; got != "structured" {
					t.Fatalf("claude stdio env ESHU_MCP_RESULT_SHAPE = %q, want structured:\n%s", got, out)
				}
			case "codex":
				if !strings.Contains(out, `env = { ESHU_MCP_RESULT_SHAPE = "structured" }`) {
					t.Fatalf("codex stdio snippet missing the shape env:\n%s", out)
				}
			default:
				if strings.Contains(out, "ESHU_MCP_RESULT_SHAPE") {
					t.Fatalf("%s stdio snippet must stay on dual:\n%s", name, out)
				}
			}
		})
	}
}

// TestWriteTargetEntryMatchesTheSnippetShape proves the entry --write merges
// into a client config carries the same shape as the printed snippet.
func TestWriteTargetEntryMatchesTheSnippetShape(t *testing.T) {
	t.Parallel()
	req := SetupRequest{Mode: ModeHostedHTTP, ServiceURL: "https://your-eshu-host", Posture: PostureSSO}
	for name, want := range map[string]string{
		"claude": "https://your-eshu-host/mcp/message?shape=structured",
		"cursor": "https://your-eshu-host/mcp/message",
		"vscode": "https://your-eshu-host/mcp/message",
	} {
		p, err := ResolvePlatform(name)
		if err != nil {
			t.Fatalf("ResolvePlatform(%q): %v", name, err)
		}
		if got := serverEntry(p, req)["url"]; got != want {
			t.Fatalf("%s entry url = %v, want %q", name, got, want)
		}
	}
}
