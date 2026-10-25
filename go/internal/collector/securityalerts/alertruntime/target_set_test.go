// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// testTargetSetHash is the digest input a target with these collected
// repositories contributes.
func testTargetSetHash(slugs ...string) string {
	return scope.NewSecurityAlertTargetSet(slugs).Hash()
}

// collectOrgTarget runs one claim of an org target with the given allowlist
// and a provider answer that never changes.
func collectOrgTarget(t *testing.T, allowed []string, generation string) collector.CollectedGeneration {
	t.Helper()

	now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
	client := staticAlertClient{result: securityalerts.GitHubDependabotAlertResult{
		Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(7, "example-org/alpha-repo")},
		PagesFetched: 1,
		ObservedAt:   now,
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			Scope:               TargetScopeOrganization,
			ScopeID:             "security-alert:github-org:example-org",
			Organization:        "example-org",
			Token:               "github-token",
			AllowedRepositories: allowed,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	item := testSecurityAlertWorkItem(now)
	item.ScopeID = "security-alert:github-org:example-org"
	item.AcceptanceUnitID = item.ScopeID
	item.GenerationID = generation
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v, err %v; want ok, nil", ok, err)
	}
	_ = drainFacts(collected.Facts)
	return collected
}

// TestClaimedSourceRecordsOrgTargetSet proves an org generation records the
// allowlisted repositories of its own organization, sorted, and never an
// allowlist entry of another owner, which the org stream can never carry.
func TestClaimedSourceRecordsOrgTargetSet(t *testing.T) {
	t.Parallel()

	collected := collectOrgTarget(t, []string{"example-org/beta-repo", "Example-Org/Alpha-Repo", "other-org/gamma"}, "g1")
	got := collected.Generation.SecurityAlertTargetSet.Slugs()
	if want := []string{"example-org/alpha-repo", "example-org/beta-repo"}; !slices.Equal(got, want) {
		t.Fatalf("SecurityAlertTargetSet = %v, want %v", got, want)
	}
}

// TestClaimedSourceAllowlistChangeMovesFreshnessHint is the #8838 regression:
// an allowlist entry added between two cycles, for a repository with no alerts,
// leaves the provider answer unchanged. Unless the target set moves the
// freshness hint, the ingestion store skips the second generation as unchanged
// and no generation ever records the added repository.
func TestClaimedSourceAllowlistChangeMovesFreshnessHint(t *testing.T) {
	t.Parallel()

	first := collectOrgTarget(t, []string{"example-org/alpha-repo"}, "g1")
	again := collectOrgTarget(t, []string{"example-org/alpha-repo"}, "g2")
	added := collectOrgTarget(t, []string{"example-org/alpha-repo", "example-org/new-repo"}, "g3")

	if first.Generation.FreshnessHint == "" {
		t.Fatal("FreshnessHint is blank, want digest")
	}
	if got, want := again.Generation.FreshnessHint, first.Generation.FreshnessHint; got != want {
		t.Fatalf("FreshnessHint moved for an unchanged allowlist: %q != %q", got, want)
	}
	if added.Generation.FreshnessHint == first.Generation.FreshnessHint {
		t.Fatal("FreshnessHint did not move after an allowlist entry was added")
	}
	if !slices.Contains(added.Generation.SecurityAlertTargetSet.Slugs(), "example-org/new-repo") {
		t.Fatalf("SecurityAlertTargetSet = %v, want it to hold the added repository",
			added.Generation.SecurityAlertTargetSet.Slugs())
	}
}

// TestClaimedSourceRepositoryTargetSetIsItsRepository proves a repository
// target records exactly its repository, so a reused scope_id that now names a
// different repository moves the set, and the hint, even with zero alerts.
func TestClaimedSourceRepositoryTargetSetIsItsRepository(t *testing.T) {
	t.Parallel()

	collect := func(repository string) collector.CollectedGeneration {
		now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
		source, err := NewClaimedSource(SourceConfig{
			CollectorInstanceID: "security-alert-primary",
			Targets: []TargetConfig{{
				Provider:            ProviderGitHubDependabot,
				ScopeID:             "security-alert:github:example-org/example-repo",
				Repository:          repository,
				Token:               "github-token",
				AllowedRepositories: []string{repository},
			}},
			ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
				return staticAlertClient{result: securityalerts.GitHubDependabotAlertResult{ObservedAt: now}}, nil
			},
			Now: func() time.Time { return now },
		})
		if err != nil {
			t.Fatalf("NewClaimedSource() error = %v, want nil", err)
		}
		collected, ok, err := source.NextClaimed(context.Background(), testSecurityAlertWorkItem(now))
		if err != nil || !ok {
			t.Fatalf("NextClaimed() = ok %v, err %v; want ok, nil", ok, err)
		}
		_ = drainFacts(collected.Facts)
		return collected
	}

	original := collect("example-org/example-repo")
	reused := collect("example-org/other-repo")
	if got, want := original.Generation.SecurityAlertTargetSet.Slugs(), []string{"example-org/example-repo"}; !slices.Equal(got, want) {
		t.Fatalf("SecurityAlertTargetSet = %v, want %v", got, want)
	}
	if original.Generation.FreshnessHint == reused.Generation.FreshnessHint {
		t.Fatal("FreshnessHint did not move when the reused scope_id named another repository")
	}
}
