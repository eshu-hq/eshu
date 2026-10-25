// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"slices"
	"testing"
)

// The collector and the workflow validator accept only scope "org" for an
// organization target; the decoder must treat that value as an organization,
// or an anchored read never finds the covering org target.
func TestSecurityAlertOrgTargetWithCollectorScopeTokenCoversItsRepositories(t *testing.T) {
	t.Parallel()

	targets := decodeSecurityAlertTargets([]string{
		`{"targets":[{"provider":"github_dependabot","scope":"org","scope_id":"sa-org","organization":"Example-Org","allowed_repositories":["example-org/Example-Repo"]}]}`,
	})
	if got := securityAlertTargetScopeIDs(targets, []string{"example-org/example-repo"}, true); !slices.Equal(got, []string{"sa-org"}) {
		t.Fatalf("covering scope ids = %v, want [sa-org]", got)
	}
}

// An operator scope_id with the reserved shard marker is refused by the
// collector, so the query decoder drops it too (#9019).
func TestSecurityAlertDecoderDropsShardMarkerScopeID(t *testing.T) {
	t.Parallel()

	targets := decodeSecurityAlertTargets([]string{
		`{"targets":[{"scope":"repository","scope_id":"sa#shard-01","repository":"acme/a"},{"scope":"repository","scope_id":"sa-ok","repository":"acme/a"}]}`,
	})
	if got := securityAlertTargetScopeIDs(targets, nil, false); !slices.Equal(got, []string{"sa-ok"}) {
		t.Fatalf("scope ids = %v, want [sa-ok]", got)
	}
}
