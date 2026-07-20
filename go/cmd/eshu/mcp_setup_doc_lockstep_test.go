// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// TestDocLockstepLiterals pins the exact literal strings
// docs/public/operate/mcp-client-auth.md's client matrix embeds -- hardcoded
// here rather than built from the mcpTokenEnvVar/apiKeyEnvVar constants, so a
// future rename of either constant's VALUE without an accompanying doc update
// fails this test. scripts/verify-mcp-client-auth-doc.sh greps the doc itself
// for the same four literals; together the two sides form the lockstep guard
// (issue #5169, F-8): the doc's copy of a literal and the command's actual
// output must independently agree with this pinned set.
func TestDocLockstepLiterals(t *testing.T) {
	t.Parallel()

	t.Run("token posture env var reference", func(t *testing.T) {
		t.Parallel()
		out := renderForTest(t, "claude", mcpSetupRequest{
			Mode:       modeHostedHTTP,
			ServiceURL: "https://your-eshu-host",
			Posture:    postureToken,
		})
		if !strings.Contains(out, "${ESHU_MCP_TOKEN}") {
			t.Fatalf("token posture output missing literal ${ESHU_MCP_TOKEN}:\n%s", out)
		}
	})

	t.Run("hosted endpoint path", func(t *testing.T) {
		t.Parallel()
		out := renderForTest(t, "generic", mcpSetupRequest{
			Mode:       modeHostedHTTP,
			ServiceURL: "https://your-eshu-host",
			Posture:    postureToken,
		})
		if !strings.Contains(out, "/mcp/message") {
			t.Fatalf("hosted output missing literal /mcp/message endpoint path:\n%s", out)
		}
	})

	t.Run("codex bearer_token_env_var literal", func(t *testing.T) {
		t.Parallel()
		out := renderForTest(t, "codex", mcpSetupRequest{
			Mode:       modeHostedHTTP,
			ServiceURL: "https://your-eshu-host",
			Posture:    postureToken,
		})
		if !strings.Contains(out, `bearer_token_env_var = "ESHU_MCP_TOKEN"`) {
			t.Fatalf("codex output missing literal bearer_token_env_var = \"ESHU_MCP_TOKEN\":\n%s", out)
		}
	})

	t.Run("shared-key warning first line", func(t *testing.T) {
		t.Parallel()
		out := renderForTest(t, "claude", mcpSetupRequest{
			Mode:       modeHostedHTTP,
			ServiceURL: "https://your-eshu-host",
			Posture:    postureSharedKey,
		})
		if !strings.Contains(out, "WARNING: the shared ESHU_API_KEY is an admin/dev credential: full AllScopes") {
			t.Fatalf("shared-key output missing literal warning first line:\n%s", out)
		}
	})
}
