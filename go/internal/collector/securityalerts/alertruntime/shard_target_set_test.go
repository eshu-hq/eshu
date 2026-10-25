// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
)

// shardHint claims scopeID of an org target over allowed and returns the
// generation's freshness hint, which the ingestion store compares to skip an
// unchanged generation.
func shardHint(t *testing.T, allowed []string, scopeID string) string {
	t.Helper()
	client := &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider: ProviderGitHubDependabot, Scope: TargetScopeOrganization, ScopeID: fanoutTestOrgScopeID,
			Organization: "example-org", Token: "github-token", AllowedRepositories: allowed,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:           func() time.Time { return time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	collected, ok, err := source.NextClaimed(context.Background(), shardWorkItem(scopeID, scopeID))
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%v err=%v", scopeID, ok, err)
	}
	drainFacts(collected.Facts)
	return collected.Generation.FreshnessHint
}

// TestShardTargetSetMovesOnlyItsOwnShardsHint is the #8838 x #9019
// interaction: each shard records its own repository set, and the set's hash
// is in the freshness digest. Adding a repository that lands in another shard
// leaves this shard's hint unchanged, so the ingestion store still skips this
// shard's unchanged generation; adding one that lands in this shard moves its
// hint, so the new repository is recorded.
func TestShardTargetSetMovesOnlyItsOwnShardsHint(t *testing.T) {
	t.Parallel()

	allowed := make([]string, 0, 251)
	for i := range 250 {
		allowed = append(allowed, fmt.Sprintf("example-org/svc-%03d", i))
	}
	leaves := shard.Partition(fanoutTestOrgScopeID, "example-org", allowed)
	target := leaves[0]
	var elsewhere, inside string
	for i := 0; elsewhere == "" || inside == ""; i++ {
		candidate := fmt.Sprintf("example-org/added-%04d", i)
		grown := shard.Partition(fanoutTestOrgScopeID, "example-org", append(slices.Clone(allowed), candidate))
		if len(grown) != len(leaves) {
			continue // the add split a leaf; pick one that keeps the shard set
		}
		home, _ := shard.For(grown, candidate)
		if home.ScopeID == target.ScopeID && inside == "" {
			inside = candidate
		} else if home.ScopeID != target.ScopeID && elsewhere == "" {
			elsewhere = candidate
		}
	}
	before := shardHint(t, allowed, target.ScopeID)
	if got := shardHint(t, append(slices.Clone(allowed), elsewhere), target.ScopeID); got != before {
		t.Fatalf("adding %s to another shard moved %s's hint: %s -> %s", elsewhere, target.ScopeID, before, got)
	}
	if got := shardHint(t, append(slices.Clone(allowed), inside), target.ScopeID); got == before {
		t.Fatalf("adding %s to %s left its hint unchanged; the new repository would never be recorded", inside, target.ScopeID)
	}
}
