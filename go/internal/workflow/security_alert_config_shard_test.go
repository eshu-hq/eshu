// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
)

// An operator scope_id containing the shard marker could collide with a
// generated org shard scope id (#9019), so validation refuses it on every
// target kind.
func TestValidateSecurityAlertCollectorConfigurationRejectsShardMarkerScopeID(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"org": `{"targets":[{"provider":"github_dependabot","scope":"org","scope_id":"security-alert:github-org:acme#shard-01",
			"organization":"acme","token_env":"GITHUB_TOKEN","allowed_repositories":["acme/a"]}]}`,
		"repository": `{"targets":[{"provider":"github_dependabot","scope_id":"x#shard-","repository":"acme/a",
			"token_env":"GITHUB_TOKEN","allowed_repositories":["acme/a"]}]}`,
	} {
		err := ValidateSecurityAlertCollectorConfiguration(raw)
		if err == nil || !strings.Contains(err.Error(), "#shard-") {
			t.Fatalf("%s: error = %v, want the reserved #shard- marker refusal", name, err)
		}
	}
}
