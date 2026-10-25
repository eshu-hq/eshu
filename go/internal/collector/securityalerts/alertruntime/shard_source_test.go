// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// shardTestFixture is one org target of n example-org repositories (plus one
// other-owner entry that never counts), its org read answering one alert per
// repository, and a source that records every read state it is handed.
type shardTestFixture struct {
	allowed []string
	client  *fanoutClient
	source  *ClaimedSource
	logs    *bytes.Buffer
	spans   *tracetest.SpanRecorder
	mu      sync.Mutex
	states  []securityalerts.TargetReadState
}

func newShardTestFixture(t *testing.T, n int) *shardTestFixture {
	t.Helper()
	f := &shardTestFixture{logs: &bytes.Buffer{}, spans: tracetest.NewSpanRecorder()}
	alerts := make([]securityalerts.GitHubDependabotAlert, 0, n)
	for i := range n {
		repository := fmt.Sprintf("example-org/repo-%04d", i)
		f.allowed = append(f.allowed, repository)
		alerts = append(alerts, orgAlert(i+1, repository))
	}
	f.allowed = append(f.allowed, "other-org/never")
	f.client = &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{Alerts: alerts, PagesFetched: 1}}
	now := time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider: ProviderGitHubDependabot, Scope: TargetScopeOrganization, ScopeID: fanoutTestOrgScopeID,
			Organization: "example-org", Token: "github-token", MaxPages: 1, AllowedRepositories: f.allowed,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return f.client, nil },
		Now:           func() time.Time { return now },
		Tracer:        sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(f.spans)).Tracer("test"),
		Logger:        slog.New(slog.NewJSONHandler(f.logs, nil)),
		ReadStateWriter: func(state securityalerts.TargetReadState) collector.GenerationStateWriter {
			f.mu.Lock()
			f.states = append(f.states, state)
			f.mu.Unlock()
			return func(context.Context, collector.CommitExecutor) error { return nil }
		},
		FanoutCursorReader: emptyCursorReader{},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	f.source = source
	return f
}

type emptyCursorReader struct{}

func (emptyCursorReader) FanoutCursor(context.Context, string) (securityalerts.FanoutCursor, error) {
	return securityalerts.FanoutCursor{}, nil
}

func shardWorkItem(scopeID, acceptanceUnitID string) workflow.WorkItem {
	item := testSecurityAlertWorkItem(time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC))
	item.ScopeID, item.AcceptanceUnitID = scopeID, acceptanceUnitID
	item.GenerationID = "security_alert:gen-" + scopeID
	item.SourceRunID = item.GenerationID
	return item
}

func (f *shardTestFixture) claim(t *testing.T, scopeID, acceptanceUnitID string) (collector.CollectedGeneration, []string, error) {
	t.Helper()
	collected, ok, err := f.source.NextClaimed(context.Background(), shardWorkItem(scopeID, acceptanceUnitID))
	if err != nil {
		return collected, nil, err
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false")
	}
	var repositories []string
	for _, env := range drainFacts(collected.Facts) {
		repositories = append(repositories, strings.TrimPrefix(payloadString(env.Payload, "repository_id"), "security-alert:github:"))
	}
	slices.Sort(repositories)
	return collected, repositories, nil
}

func TestShardedOrgTargetCollectsEachShardWithItsOwnRepositories(t *testing.T) {
	t.Parallel()

	f := newShardTestFixture(t, 150)
	leaves := shard.Partition(fanoutTestOrgScopeID, "example-org", f.allowed)
	if len(leaves) < 2 {
		t.Fatalf("150 repositories gave %d shards", len(leaves))
	}
	seen := map[string]bool{}
	for _, leaf := range leaves {
		collected, repositories, err := f.claim(t, leaf.ScopeID, leaf.ScopeID)
		if err != nil {
			t.Fatalf("claim %s: %v", leaf.ScopeID, err)
		}
		if collected.Scope.ScopeID != leaf.ScopeID || collected.Scope.ParentScopeID != fanoutTestOrgScopeID {
			t.Fatalf("scope = %q parent %q, want %q under %q", collected.Scope.ScopeID, collected.Scope.ParentScopeID, leaf.ScopeID, fanoutTestOrgScopeID)
		}
		if collected.Generation.TrustedDeletion {
			t.Fatal("a collection generation carries TrustedDeletion")
		}
		if want := slices.Sorted(slices.Values(leaf.Repositories)); !slices.Equal(repositories, want) {
			t.Fatalf("shard %s facts cover %d repositories, want its %d", leaf.ScopeID, len(repositories), len(want))
		}
		for _, repository := range repositories {
			if seen[repository] {
				t.Fatalf("repository %s collected by two shards", repository)
			}
			seen[repository] = true
		}
	}
	if len(seen) != 150 {
		t.Fatalf("shards collected %d repositories, want 150", len(seen))
	}
	for i, state := range f.states {
		if state.RepositoriesTotal != len(leaves[i].Repositories) || state.ScopeID != leaves[i].ScopeID {
			t.Fatalf("read state %d = %s total %d, want %s total %d", i, state.ScopeID, state.RepositoriesTotal, leaves[i].ScopeID, len(leaves[i].Repositories))
		}
	}
}

func TestUnshardedOrgTargetKeepsItsScopeAndFullAllowlist(t *testing.T) {
	t.Parallel()

	f := newShardTestFixture(t, 100)
	collected, repositories, err := f.claim(t, fanoutTestOrgScopeID, fanoutTestOrgScopeID)
	if err != nil {
		t.Fatalf("claim base: %v", err)
	}
	if collected.Scope.ParentScopeID != "" || len(repositories) != 100 {
		t.Fatalf("base claim parent %q, %d repositories; want no parent and 100", collected.Scope.ParentScopeID, len(repositories))
	}
}

func TestBaseClaimOfAShardedTargetFailsRetryableForAnOldCoordinator(t *testing.T) {
	t.Parallel()

	f := newShardTestFixture(t, 101)
	_, _, err := f.claim(t, fanoutTestOrgScopeID, fanoutTestOrgScopeID)
	var failure ProviderFailure
	if !errors.As(err, &failure) || failure.FailureClass() != FailureRetryable || !errors.Is(err, ErrShardedTargetNeedsCoordinatorUpgrade) {
		t.Fatalf("base claim of a sharded target = %v, want a retryable upgrade-the-coordinator failure", err)
	}
	if f.client.orgCalls != 0 {
		t.Fatalf("org reads = %d, want none: the guard neither collects nor retires", f.client.orgCalls)
	}
	if !strings.Contains(f.logs.String(), "rolling restart") || !strings.Contains(f.logs.String(), "security_alert.target_sharded_coordinator_skew") {
		t.Fatalf("skew guard log missing: %s", f.logs.String())
	}
}

func TestRetirementClaimCommitsAnAttestedEmptyGeneration(t *testing.T) {
	t.Parallel()

	f := newShardTestFixture(t, 101)
	collected, repositories, err := f.claim(t, fanoutTestOrgScopeID, shard.RetirementAcceptanceUnitID(fanoutTestOrgScopeID))
	if err != nil {
		t.Fatalf("retirement claim: %v", err)
	}
	if len(repositories) != 0 || f.client.orgCalls != 0 {
		t.Fatalf("retirement emitted %d facts after %d provider reads, want none", len(repositories), f.client.orgCalls)
	}
	generation := collected.Generation
	if !generation.TrustedDeletion || generation.FreshnessHint != RetirementFreshnessHint || generation.ScopeID != fanoutTestOrgScopeID {
		t.Fatalf("retirement generation = %+v", generation)
	}
	if collected.Scope.ScopeID != fanoutTestOrgScopeID || collected.Scope.ParentScopeID != "" {
		t.Fatalf("retirement scope = %+v", collected.Scope)
	}
	if collected.StateWriter == nil || len(f.states) != 1 || f.states[0].RepositoriesTotal != 0 || f.states[0].ScopeID != fanoutTestOrgScopeID {
		t.Fatalf("retirement read state = %+v, want one row with repositories_total 0", f.states)
	}
	if !strings.Contains(f.logs.String(), "security_alert.scope_retired") {
		t.Fatalf("retirement log missing: %s", f.logs.String())
	}
}

func TestRetirementOfAMergedShardKeepsItsParent(t *testing.T) {
	t.Parallel()

	f := newShardTestFixture(t, 100)
	merged := shard.ScopeID(fanoutTestOrgScopeID, "1")
	collected, _, err := f.claim(t, merged, shard.RetirementAcceptanceUnitID(merged))
	if err != nil {
		t.Fatalf("retirement claim: %v", err)
	}
	if collected.Scope.ScopeID != merged || collected.Scope.ParentScopeID != fanoutTestOrgScopeID {
		t.Fatalf("retired shard scope = %+v", collected.Scope)
	}
}

func TestRetirementIsRefusedForAPlannedOrUnknownScope(t *testing.T) {
	t.Parallel()

	f := newShardTestFixture(t, 101)
	planned := shard.Partition(fanoutTestOrgScopeID, "example-org", f.allowed)[0].ScopeID
	for _, scopeID := range []string{planned, "security-alert:github-org:unknown#shard-0", "security-alert:github:example-org/x"} {
		_, _, err := f.claim(t, scopeID, shard.RetirementAcceptanceUnitID(scopeID))
		var failure ProviderFailure
		if !errors.As(err, &failure) || failure.FailureClass() != FailureRetryable {
			t.Fatalf("retirement of %q = %v, want a retryable refusal", scopeID, err)
		}
	}
	if len(f.states) != 0 {
		t.Fatalf("refused retirements wrote %d read states", len(f.states))
	}
}

func TestShardAndRetirementClaimsCarryShardSpanAttributes(t *testing.T) {
	t.Parallel()

	f := newShardTestFixture(t, 150)
	leaves := shard.Partition(fanoutTestOrgScopeID, "example-org", f.allowed)
	if _, _, err := f.claim(t, leaves[0].ScopeID, leaves[0].ScopeID); err != nil {
		t.Fatalf("shard claim: %v", err)
	}
	if _, _, err := f.claim(t, fanoutTestOrgScopeID, shard.RetirementAcceptanceUnitID(fanoutTestOrgScopeID)); err != nil {
		t.Fatalf("retirement claim: %v", err)
	}
	var observed []map[string]string
	for _, span := range f.spans.Ended() {
		if span.Name() != contract.SpanSecurityAlertObserve {
			continue
		}
		attrs := map[string]string{}
		for _, kv := range span.Attributes() {
			attrs[string(kv.Key)] = kv.Value.String()
		}
		observed = append(observed, attrs)
	}
	if len(observed) != 2 {
		t.Fatalf("observe spans = %d, want 2", len(observed))
	}
	shardSpan, retireSpan := observed[0], observed[1]
	if shardSpan[contract.AttrSecurityAlertShardCount] != fmt.Sprint(len(leaves)) ||
		shardSpan[contract.AttrSecurityAlertShardRepositories] != fmt.Sprint(len(leaves[0].Repositories)) ||
		shardSpan[contract.AttrSecurityAlertRetirement] != "false" {
		t.Fatalf("shard observe span attributes = %v", shardSpan)
	}
	if retireSpan[contract.AttrSecurityAlertRetirement] != "true" || retireSpan[contract.AttrSecurityAlertShardCount] != fmt.Sprint(len(leaves)) {
		t.Fatalf("retirement observe span attributes = %v", retireSpan)
	}
	for _, attrs := range observed {
		for key, value := range attrs {
			if strings.Contains(value, "example-org/repo-") {
				t.Fatalf("span attribute %s carries a repository name: %s", key, value)
			}
		}
	}
}

func TestShardsOfOneTargetCollectConcurrentlyWithoutSharedState(t *testing.T) {
	t.Parallel()

	f := newShardTestFixture(t, 250)
	leaves := shard.Partition(fanoutTestOrgScopeID, "example-org", f.allowed)
	var wg sync.WaitGroup
	results := make([][]string, len(leaves))
	errs := make([]error, len(leaves))
	for i, leaf := range leaves {
		wg.Go(func() {
			collected, ok, err := f.source.NextClaimed(context.Background(), shardWorkItem(leaf.ScopeID, leaf.ScopeID))
			if err != nil || !ok {
				errs[i] = fmt.Errorf("claim %s: ok=%v err=%v", leaf.ScopeID, ok, err)
				return
			}
			for _, env := range drainFacts(collected.Facts) {
				if env.ScopeID != leaf.ScopeID {
					errs[i] = fmt.Errorf("fact of %s carries scope %s", leaf.ScopeID, env.ScopeID)
				}
				results[i] = append(results[i], payloadString(env.Payload, "repository_id"))
			}
		})
	}
	wg.Wait()
	total := 0
	for i := range leaves {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		total += len(results[i])
	}
	if total != 250 {
		t.Fatalf("concurrent shard claims emitted %d facts, want 250", total)
	}
}
