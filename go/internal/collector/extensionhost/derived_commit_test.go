// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// derivedCommitSource builds a Source whose runner returns result, with the
// resolver and static config under test.
func derivedCommitSource(
	t *testing.T,
	resolver DerivedTargetResolver,
	config map[string]any,
	result sdkcollector.Result,
) *Source {
	t.Helper()
	source, err := NewSource(Config{
		Manifest:              testManifest(),
		CollectorInstanceID:   "scorecard-instance",
		ScopeKind:             scope.KindRepository,
		ConfigHandle:          "cfg-scorecard",
		Config:                config,
		Runner:                &recordingRunner{result: result},
		Clock:                 testObservedAt,
		DerivedTargetResolver: resolver,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	return source
}

// TestNextClaimedStampsTheDerivedCommitOnTheGeneration is decision 5 of the
// #10055 ruling: a committed derived generation carries the commit the coordinator
// planned, which is the evidence commit the skew fence compares against.
func TestNextClaimedStampsTheDerivedCommitOnTheGeneration(t *testing.T) {
	t.Parallel()

	item := goClaim(testGoScope)
	source := derivedCommitSource(t,
		GoReachabilityTargetResolver(&fakeRunScopeSetReader{raw: goScopeSet("https://github.com/eshu-hq/widgets")}),
		nil, completeResult(item))

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok = %v, error = %v", ok, err)
	}
	if got := collected.Generation.SourceCommitSHA; got != testGoCommit {
		t.Fatalf("SourceCommitSHA = %q, want the planned commit %q", got, testGoCommit)
	}
}

// TestNextClaimedLeavesOtherGenerationsWithoutACommit proves every other scope's
// generation is unchanged. That covers a scope the resolver declines, a source
// with no resolver, and a static config entry that spells the reserved key: only
// the host's resolver can name the commit, so config cannot forge one.
func TestNextClaimedLeavesOtherGenerationsWithoutACommit(t *testing.T) {
	t.Parallel()

	forged := map[string]any{HostDerivedTargetKey: map[string]any{"source_commit_sha": testGoCommit}}
	for _, tc := range []struct {
		name     string
		resolver DerivedTargetResolver
		config   map[string]any
		scopeID  string
	}{
		{
			name:     "resolver declines another scope",
			resolver: GoReachabilityTargetResolver(&fakeRunScopeSetReader{}),
			scopeID:  "vuln-intel://cisa/kev",
		},
		{name: "no resolver", scopeID: testGoScope},
		{name: "no resolver and forged static config", config: forged, scopeID: testGoScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := goClaim(tc.scopeID)
			collected, ok, err := derivedCommitSource(t, tc.resolver, tc.config, completeResult(item)).
				NextClaimed(context.Background(), item)
			if err != nil || !ok {
				t.Fatalf("NextClaimed() ok = %v, error = %v", ok, err)
			}
			if got := collected.Generation.SourceCommitSHA; got != "" {
				t.Fatalf("SourceCommitSHA = %q, want empty", got)
			}
		})
	}
}
