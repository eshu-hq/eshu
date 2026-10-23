// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestOnboardingToolCountNamesItsScope proves the onboarding Markdown and the
// setup terminal summary say the tool count is this build's registered tools,
// and that a deployment can advertise fewer, so a router-surface operator is
// not misled by the number.
func TestOnboardingToolCountNamesItsScope(t *testing.T) {
	t.Parallel()

	t.Run("markdown", func(t *testing.T) {
		t.Parallel()
		markdown, err := RenderArtifactMarkdown(Artifact{Connection: Connection{ToolCount: 163}})
		if err != nil {
			t.Fatalf("RenderArtifactMarkdown err = %v, want nil", err)
		}
		for _, want := range []string{"registered in this build", "`163`", "ESHU_MCP_TOOL_SURFACE"} {
			if !strings.Contains(markdown, want) {
				t.Fatalf("markdown = %q, want it to contain %q", markdown, want)
			}
		}
		if strings.Contains(markdown, "MCP tools visible") {
			t.Fatalf("markdown still says %q", "MCP tools visible")
		}
	})

	t.Run("setup terminal", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		RenderSetupHuman(&out, Result{ToolCount: 163}, nil)
		got := out.String()
		if !strings.Contains(got, "163 registered in this build") {
			t.Fatalf("terminal = %q, want it to say %q", got, "163 registered in this build")
		}
		if strings.Contains(got, "163 visible") {
			t.Fatalf("terminal still says %q", "163 visible")
		}
	})

	t.Run("json field name is stable", func(t *testing.T) {
		t.Parallel()
		raw, err := RenderArtifactJSON(Artifact{Connection: Connection{ToolCount: 5}})
		if err != nil {
			t.Fatalf("RenderArtifactJSON err = %v, want nil", err)
		}
		var decoded struct {
			Connection map[string]any `json:"connection"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got, _ := decoded.Connection["tool_count"].(float64); got != 5 {
			t.Fatalf("connection.tool_count = %v, want 5", decoded.Connection["tool_count"])
		}
	})
}
