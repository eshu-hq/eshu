// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const testSlotScopeID = "vuln-intel://osv/npm/_slot/3/0"

func slotQueries() []sdkcollector.ScopeQuery {
	return []sdkcollector.ScopeQuery{
		{Ecosystem: "npm", Package: "left-pad", Version: "1.3.0"},
		{Ecosystem: "npm", Package: "is-odd", Version: "3.0.1"},
	}
}

// TestRequestForWorkItemCarriesResolvedScopeQueries proves item 1 of
// eshu-hq/eshu#9432: a claim for a sharded scope delivers the run's
// requested_scope_set entry on the claim the satellite receives.
func TestRequestForWorkItemCarriesResolvedScopeQueries(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	source := mustNewSource(t, runner, &recordingStatusRecorder{})
	source.scopeQueryResolver = func(context.Context, workflow.WorkItem) ([]sdkcollector.ScopeQuery, error) {
		return slotQueries(), nil
	}
	item := testWorkItem()
	item.ScopeID = testSlotScopeID

	request, err := source.requestForWorkItem(context.Background(), item)
	if err != nil {
		t.Fatalf("requestForWorkItem() error = %v, want nil", err)
	}
	if len(request.Claim.ScopeQueries) != 2 {
		t.Fatalf("ScopeQueries count = %d, want 2", len(request.Claim.ScopeQueries))
	}
	if request.Claim.ScopeQueries[0].Package != "left-pad" || request.Claim.ScopeQueries[0].Version != "1.3.0" {
		t.Fatalf("ScopeQueries[0] = %+v, want left-pad@1.3.0", request.Claim.ScopeQueries[0])
	}
}

// TestRequestForWorkItemOmitsScopeQueriesWithoutResolver proves the
// old-satellite wire stays byte-identical when no transport is configured.
func TestRequestForWorkItemOmitsScopeQueriesWithoutResolver(t *testing.T) {
	t.Parallel()

	source := mustNewSource(t, &recordingRunner{}, &recordingStatusRecorder{})
	request, err := source.requestForWorkItem(context.Background(), testWorkItem())
	if err != nil {
		t.Fatalf("requestForWorkItem() error = %v, want nil", err)
	}
	if request.Claim.ScopeQueries != nil {
		t.Fatalf("ScopeQueries = %+v, want nil", request.Claim.ScopeQueries)
	}
	raw, err := json.Marshal(request.Claim)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "scope_queries") {
		t.Fatalf("legacy claim carries scope_queries: %s", raw)
	}
}

// TestNextClaimedResolverFailureIsTerminalInvalidClaim proves a broken
// transport fails the claim before the satellite ever launches: retry
// cannot produce queries.
func TestNextClaimedResolverFailureIsTerminalInvalidClaim(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{result: completeResult(testWorkItem())}
	source := mustNewSource(t, runner, &recordingStatusRecorder{})
	source.scopeQueryResolver = func(context.Context, workflow.WorkItem) ([]sdkcollector.ScopeQuery, error) {
		return nil, errors.New("workflow run row missing")
	}

	_, _, err := source.NextClaimed(context.Background(), testWorkItem())
	assertFailure(t, err, FailureClassInvalidClaim, true)
	if len(runner.requests) != 0 {
		t.Fatalf("runner invoked %d times, want 0 (satellite must not launch)", len(runner.requests))
	}
}

// TestNextClaimedDeliversScopeQueriesToRunner proves the claim the runner
// receives carries the resolved queries end to end through NextClaimed.
func TestNextClaimedDeliversScopeQueriesToRunner(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	item.ScopeID = testSlotScopeID
	runner := &recordingRunner{}
	source := mustNewSource(t, runner, &recordingStatusRecorder{})
	source.scopeQueryResolver = func(context.Context, workflow.WorkItem) ([]sdkcollector.ScopeQuery, error) {
		return slotQueries(), nil
	}
	runner.result = completeResult(item)
	runner.result.Claim.ScopeQueries = slotQueries()

	if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner invocations = %d, want 1", len(runner.requests))
	}
	if len(runner.requests[0].Claim.ScopeQueries) != 2 {
		t.Fatalf("runner claim ScopeQueries count = %d, want 2", len(runner.requests[0].Claim.ScopeQueries))
	}
}

// BenchmarkRequestForWorkItemWithoutResolver is the no-transport baseline
// for the claim-transport no-regression note: building a legacy claim with
// no resolver configured.
func BenchmarkRequestForWorkItemWithoutResolver(b *testing.B) {
	source := mustNewSourceWithManifest(b, testManifest(), &recordingRunner{}, &recordingStatusRecorder{})
	item := testWorkItem()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := source.requestForWorkItem(context.Background(), item); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRequestForWorkItemWithScopeQueries measures claim building with
// transport configured: the resolver delivers two queries and the claim
// carries them. See the no-regression note in README.md.
func BenchmarkRequestForWorkItemWithScopeQueries(b *testing.B) {
	source := mustNewSourceWithManifest(b, testManifest(), &recordingRunner{}, &recordingStatusRecorder{})
	source.scopeQueryResolver = func(context.Context, workflow.WorkItem) ([]sdkcollector.ScopeQuery, error) {
		return slotQueries(), nil
	}
	item := testWorkItem()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := source.requestForWorkItem(context.Background(), item); err != nil {
			b.Fatal(err)
		}
	}
}

// TestValidateReturnedClaimComparesScopeQueries proves the echo check is
// strict on transport: a tampered query list fails, empty equals empty.
func TestValidateReturnedClaimComparesScopeQueries(t *testing.T) {
	t.Parallel()

	withQueries := testSDKClaim(testWorkItem())
	withQueries.ScopeQueries = slotQueries()

	if err := validateReturnedClaim(withQueries, withQueries); err != nil {
		t.Fatalf("identical scope queries rejected: %v", err)
	}
	plain := testSDKClaim(testWorkItem())
	if err := validateReturnedClaim(plain, plain); err != nil {
		t.Fatalf("empty scope queries rejected: %v", err)
	}
	stripped := withQueries
	stripped.ScopeQueries = nil
	if err := validateReturnedClaim(withQueries, stripped); err == nil {
		t.Fatal("stripped scope queries accepted, want identity mismatch")
	}
	added := plain
	added.ScopeQueries = slotQueries()
	if err := validateReturnedClaim(plain, added); err == nil {
		t.Fatal("injected scope queries accepted, want identity mismatch")
	}
	tampered := withQueries
	tampered.ScopeQueries = []sdkcollector.ScopeQuery{{Ecosystem: "npm", Package: "left-pad", Version: "9.9.9"}}
	if err := validateReturnedClaim(withQueries, tampered); err == nil {
		t.Fatal("tampered scope query version accepted, want identity mismatch")
	}
}
