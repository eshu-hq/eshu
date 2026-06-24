// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

// TestSecretsIAMToolsRegisteredAndRoutable proves every secrets/IAM read tool is
// present in ReadOnlyTools and resolves to a dispatch route, so a tool cannot be
// defined without being callable (or vice versa).
func TestSecretsIAMToolsRegisteredAndRoutable(t *testing.T) {
	t.Parallel()

	wantTools := []string{
		"list_secrets_iam_identity_trust_chains",
		"list_secrets_iam_privilege_posture_observations",
		"list_secrets_iam_secret_access_paths",
		"list_secrets_iam_posture_gaps",
		"count_secrets_iam_posture",
	}

	registered := map[string]bool{}
	for _, tool := range ReadOnlyTools() {
		registered[tool.Name] = true
	}
	for _, name := range wantTools {
		if !registered[name] {
			t.Errorf("tool %q not registered in ReadOnlyTools", name)
		}
		route, err := resolveRoute(name, map[string]any{"scope_id": "s", "limit": 10})
		if err != nil || route == nil {
			t.Errorf("tool %q does not resolve to a dispatch route: %v", name, err)
			continue
		}
		if route.method != "GET" {
			t.Errorf("tool %q route method = %q, want GET", name, route.method)
		}
	}
}
