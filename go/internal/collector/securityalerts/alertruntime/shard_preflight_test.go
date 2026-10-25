// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// preflightShardFixture is one sharded org target (250 repositories of
// example-org) beside one unsharded org target (2 repositories of other-org),
// each served by its own fake client so provider reads count per target.
type preflightShardFixture struct {
	source  *ClaimedSource
	sharded *fanoutClient
	single  *fanoutClient
	reader  func(statusClass string) int64
	shards  int
}

func newPreflightShardFixture(t *testing.T, shardedErr error) preflightShardFixture {
	t.Helper()
	allowed := make([]string, 0, 250)
	for i := range 250 {
		allowed = append(allowed, fmt.Sprintf("example-org/svc-%03d", i))
	}
	f := preflightShardFixture{
		sharded: &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1}, orgErr: shardedErr},
		single:  &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1}},
		shards:  len(shard.Partition(fanoutTestOrgScopeID, "example-org", allowed)),
	}
	instruments, metrics := newTestInstruments(t)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{
			{
				Provider: ProviderGitHubDependabot, Scope: TargetScopeOrganization, ScopeID: fanoutTestOrgScopeID,
				Organization: "example-org", Token: "github-token", AllowedRepositories: allowed,
			},
			{
				Provider: ProviderGitHubDependabot, Scope: TargetScopeOrganization, ScopeID: "security-alert:github-org:other-org",
				Organization: "other-org", Token: "github-token", AllowedRepositories: []string{"other-org/a", "other-org/b"},
			},
		},
		ClientFactory: func(target TargetConfig) (RepositoryAlertClient, error) {
			if target.Organization == "example-org" {
				return f.sharded, nil
			}
			return f.single, nil
		},
		Now:         func() time.Time { return time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC) },
		Instruments: instruments,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	f.source = source
	f.reader = func(statusClass string) int64 {
		return counterValue(t, metrics, "eshu_dp_security_alert_provider_requests_total",
			map[string]string{telemetry.MetricDimensionStatusClass: statusClass})
	}
	return f
}

// TestPreflightReadsEachShardedTargetOnce pins the #9019 preflight skip: every
// shard of a target reads the same org endpoint with the same credential, so
// preflight sends one bounded request per configured target, not one per
// shard, and reports the configured target count.
func TestPreflightReadsEachShardedTargetOnce(t *testing.T) {
	t.Parallel()

	f := newPreflightShardFixture(t, nil)
	if f.shards < 3 {
		t.Fatalf("250 repositories gave %d shards, want at least 3", f.shards)
	}
	result, err := f.source.PreflightProviderAccess(context.Background())
	if err != nil {
		t.Fatalf("PreflightProviderAccess() error = %v", err)
	}
	if result.TargetCount != 2 {
		t.Fatalf("TargetCount = %d, want the 2 configured targets, not %d runtime shards", result.TargetCount, f.shards+1)
	}
	if f.sharded.orgCalls != 1 || f.single.orgCalls != 1 {
		t.Fatalf("org reads = sharded %d, unsharded %d; want exactly 1 each (the sharded target has %d shards)",
			f.sharded.orgCalls, f.single.orgCalls, f.shards)
	}
	if len(f.sharded.repoCalls)+len(f.single.repoCalls) != 0 {
		t.Fatal("preflight read a repository endpoint")
	}
	if got := f.reader("success"); got != 2 {
		t.Fatalf("provider requests recorded as success = %d, want 2 (one per target)", got)
	}
}

// TestPreflightRecordsASharedTargetFailureOnce: when the sharded target's one
// request fails, preflight classifies and records that failure once, not once
// per shard, and stops there.
func TestPreflightRecordsASharedTargetFailureOnce(t *testing.T) {
	t.Parallel()

	f := newPreflightShardFixture(t, securityalerts.GitHubDependabotError{StatusCode: 401})
	_, err := f.source.PreflightProviderAccess(context.Background())
	var failure ProviderFailure
	if !errors.As(err, &failure) {
		t.Fatalf("PreflightProviderAccess() error = %v, want a classified provider failure", err)
	}
	if f.sharded.orgCalls != 1 {
		t.Fatalf("sharded target org reads = %d, want 1 (the target has %d shards)", f.sharded.orgCalls, f.shards)
	}
	if got := f.reader(failure.FailureClass()); got != 1 {
		t.Fatalf("provider requests recorded as %s = %d, want 1", failure.FailureClass(), got)
	}
}
