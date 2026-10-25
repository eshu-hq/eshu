// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"strings"
	"testing"
)

// A configured scope_id containing the shard marker could collide with a
// generated org shard scope id (#9019); the collector refuses it at startup.
func TestValidateTargetRejectsShardMarkerScopeID(t *testing.T) {
	t.Parallel()

	_, err := validateTarget(TargetConfig{
		Provider: ProviderGitHubDependabot, Scope: TargetScopeOrganization, ScopeID: "security-alert:github-org:acme#shard-0",
		Organization: "acme", Token: "token", AllowedRepositories: []string{"acme/a"},
	})
	if err == nil || !strings.Contains(err.Error(), "#shard-") {
		t.Fatalf("validateTarget() error = %v, want the reserved #shard- marker refusal", err)
	}
}
