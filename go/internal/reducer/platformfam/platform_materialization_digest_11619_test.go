// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package platformfam

import (
	"context"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// digestReportingCrossRepoResolver is the family-local double for a resolver
// that also reports the resolved-set digest. The digest travels beside the
// edge count so the handler can fence the deployment-evidence replay on the
// set that produced it, not on the count alone.
type digestReportingCrossRepoResolver struct {
	calls  []crossRepoResolveCall
	writes int
	digest string
	err    error
}

func (r *digestReportingCrossRepoResolver) Resolve(
	_ context.Context,
	scopeID string,
	generationID string,
) (int, error) {
	r.calls = append(r.calls, crossRepoResolveCall{scopeID: scopeID, generationID: generationID})
	return r.writes, r.err
}

func (r *digestReportingCrossRepoResolver) ResolveClaimed(
	_ context.Context,
	intent reducercontract.Intent,
) (int, error) {
	r.calls = append(r.calls, crossRepoResolveCall{
		scopeID:      intent.ScopeID,
		generationID: intent.GenerationID,
		claimedAt:    intent.ClaimedAt,
	})
	return r.writes, r.err
}

func (r *digestReportingCrossRepoResolver) ResolveWithDigest(
	_ context.Context,
	scopeID string,
	generationID string,
) (int, string, error) {
	r.calls = append(r.calls, crossRepoResolveCall{scopeID: scopeID, generationID: generationID})
	return r.writes, r.digest, r.err
}

func (r *digestReportingCrossRepoResolver) ResolveClaimedWithDigest(
	_ context.Context,
	intent reducercontract.Intent,
) (int, string, error) {
	r.calls = append(r.calls, crossRepoResolveCall{
		scopeID:      intent.ScopeID,
		generationID: intent.GenerationID,
		claimedAt:    intent.ClaimedAt,
	})
	return r.writes, r.digest, r.err
}

// recordingDigestWorkloadReplayer records both the legacy unfenced replay and
// the digest-fenced replay so a test can prove which path the handler took.
type recordingDigestWorkloadReplayer struct {
	recordingWorkloadMaterializationReplayer
	digestCalls []workloadMaterializationReplayDigestCall
}

type workloadMaterializationReplayDigestCall struct {
	scopeID      string
	generationID string
	entityKey    string
	digest       string
}

func (r *recordingDigestWorkloadReplayer) ReplayWorkloadMaterializationForDigest(
	_ context.Context,
	scopeID string,
	generationID string,
	entityKey string,
	digest string,
) (bool, error) {
	r.digestCalls = append(r.digestCalls, workloadMaterializationReplayDigestCall{
		scopeID:      scopeID,
		generationID: generationID,
		entityKey:    entityKey,
		digest:       digest,
	})
	return true, r.err
}

func digestFenceTestIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:        "intent-pm-digest",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          reducercontract.DomainDeploymentMapping,
		Cause:           "platform discovered",
		EntityKeys:      []string{"platform:ecs:aws:cluster", "repo:service-edge-api"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          reducercontract.IntentStatusClaimed,
	}
}

// Test11619DigestResolverReplaysWithResolvedSetDigest proves the handler takes
// the digest-fenced replay path when the resolver reports a digest: the
// digest reaches the replayer unchanged and the legacy call is not issued.
func Test11619DigestResolverReplaysWithResolvedSetDigest(t *testing.T) {
	t.Parallel()

	resolver := &digestReportingCrossRepoResolver{writes: 2, digest: "digest-abc-11619"}
	replayer := &recordingDigestWorkloadReplayer{}
	handler := PlatformMaterializationHandler{
		Writer:                          &recordingPlatformMaterializationWriter{},
		WorkloadMaterializationReplayer: replayer,
		CrossRepoResolver:               resolver,
	}

	if _, err := handler.Handle(context.Background(), digestFenceTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(replayer.digestCalls), 1; got != want {
		t.Fatalf("digest replayer calls = %d, want %d", got, want)
	}
	call := replayer.digestCalls[0]
	if got, want := call.digest, "digest-abc-11619"; got != want {
		t.Fatalf("digest replayer digest = %q, want %q", got, want)
	}
	if got, want := call.entityKey, "repo:service-edge-api"; got != want {
		t.Fatalf("digest replayer entity_key = %q, want %q", got, want)
	}
	if got, want := len(replayer.calls), 0; got != want {
		t.Fatalf("legacy replayer calls = %d, want %d (digest path must not fall through)", got, want)
	}
}

// Test11619LegacyResolverFallsBackToUnfencedReplay proves a resolver that
// cannot report a digest keeps today's behavior: the legacy replay fires, so
// nothing owed is skipped.
func Test11619LegacyResolverFallsBackToUnfencedReplay(t *testing.T) {
	t.Parallel()

	replayer := &recordingDigestWorkloadReplayer{}
	handler := PlatformMaterializationHandler{
		Writer:                          &recordingPlatformMaterializationWriter{},
		WorkloadMaterializationReplayer: replayer,
		CrossRepoResolver:               &recordingCrossRepoRelationshipResolver{writes: 2},
	}

	if _, err := handler.Handle(context.Background(), digestFenceTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(replayer.calls), 1; got != want {
		t.Fatalf("legacy replayer calls = %d, want %d", got, want)
	}
	if got, want := len(replayer.digestCalls), 0; got != want {
		t.Fatalf("digest replayer calls = %d, want %d", got, want)
	}
}

// Test11619EmptyDigestFallsBackToUnfencedReplay proves an empty digest from a
// digest-capable resolver is treated as unknown and keeps today's behavior.
func Test11619EmptyDigestFallsBackToUnfencedReplay(t *testing.T) {
	t.Parallel()

	replayer := &recordingDigestWorkloadReplayer{}
	handler := PlatformMaterializationHandler{
		Writer:                          &recordingPlatformMaterializationWriter{},
		WorkloadMaterializationReplayer: replayer,
		CrossRepoResolver:               &digestReportingCrossRepoResolver{writes: 2},
	}

	if _, err := handler.Handle(context.Background(), digestFenceTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(replayer.calls), 1; got != want {
		t.Fatalf("legacy replayer calls = %d, want %d", got, want)
	}
	if got, want := len(replayer.digestCalls), 0; got != want {
		t.Fatalf("digest replayer calls = %d, want %d", got, want)
	}
}

// Test11619DigestReplayerAbsentFallsBackToUnfencedReplay proves a replayer
// without the digest method keeps today's behavior even when the resolver
// reports a digest.
func Test11619DigestReplayerAbsentFallsBackToUnfencedReplay(t *testing.T) {
	t.Parallel()

	replayer := &recordingWorkloadMaterializationReplayer{}
	handler := PlatformMaterializationHandler{
		Writer:                          &recordingPlatformMaterializationWriter{},
		WorkloadMaterializationReplayer: replayer,
		CrossRepoResolver:               &digestReportingCrossRepoResolver{writes: 2, digest: "digest-abc-11619"},
	}

	if _, err := handler.Handle(context.Background(), digestFenceTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(replayer.calls), 1; got != want {
		t.Fatalf("legacy replayer calls = %d, want %d", got, want)
	}
}
