// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// commitListenerObservedAt pins the epoch the listener regression drives.
func commitListenerObservedAt() time.Time {
	return time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
}

// commitListenerManifest mirrors the scorecard fixture the extensionhost
// handler tests admit: one namespaced fact kind, so no producer grant is
// needed for the result to pass admission.
func commitListenerManifest() component.Manifest {
	return component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: component.Metadata{
			ID:        "dev.eshu.examples.scorecard",
			Name:      "Scorecard",
			Publisher: "eshu",
			Version:   "0.1.0",
		},
		Spec: component.Spec{
			CompatibleCore: ">=0.1.0",
			ComponentType:  component.ComponentTypeCollector,
			CollectorKinds: []string{"repo"},
			Runtime: component.RuntimeContract{
				SDKProtocol: component.CollectorSDKProtocolV1Alpha1,
				Adapter:     component.RuntimeAdapterProcess,
			},
			Artifacts: []component.Artifact{{
				Platform: "darwin/arm64",
				Image:    "ghcr.io/eshu-hq/scorecard@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
			EmittedFacts: []component.FactFamily{{
				Kind:             "dev.eshu.examples.scorecard.check",
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []string{"observed", "inferred"},
			}},
			ConsumerContracts: component.ConsumerContracts{
				Reducer: component.ReducerContract{Phases: []string{"component_scorecard"}},
			},
			Telemetry: component.Telemetry{MetricsPrefix: "eshu_component_scorecard"},
		},
	}
}

// commitListenerClaim returns the fenced epoch the wire carries beside the
// result: the planned scope, source run, and generation IDs the commit pin
// matches against the plane's item row.
func commitListenerClaim(now time.Time) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:    "work-scorecard-1",
		ClaimID:       "claim-1",
		FencingToken:  7,
		OwnerID:       "worker-1",
		ObservedAt:    now,
		LeaseDuration: time.Minute,
		ScopeID:       "repo-eshu",
		SourceRunID:   "generation-1",
		GenerationID:  "generation-1",
	}
}

// commitListenerResult returns one complete single-call result echoing the
// fenced epoch with no statuses: the status-free shape the commit lane
// records.
func commitListenerResult(now time.Time) sdkcollector.Result {
	return sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultComplete,
		Claim: sdkcollector.Claim{
			ComponentID:   "dev.eshu.examples.scorecard",
			InstanceID:    "scorecard-instance",
			CollectorKind: "repo",
			SourceSystem:  "git",
			Scope: sdkcollector.Scope{
				ID:   "repo-eshu",
				Kind: string(scope.KindRepository),
			},
			SourceRunID:  "generation-1",
			GenerationID: "generation-1",
			WorkItemID:   "work-scorecard-1",
			FencingToken: strconv.FormatInt(7, 10),
			Attempt:      1,
			Deadline:     now.Add(2 * time.Minute),
			ConfigHandle: "cfg-scorecard",
		},
		Generation: sdkcollector.Generation{
			ID:            "generation-1",
			ObservedAt:    now,
			FreshnessHint: "snapshot",
		},
		Facts: []sdkcollector.Fact{{
			Kind:             "dev.eshu.examples.scorecard.check",
			SchemaVersion:    "1.0.0",
			StableKey:        "scorecard:repo-eshu",
			SourceConfidence: sdkcollector.SourceConfidenceObserved,
			ObservedAt:       now,
			SourceRef: sdkcollector.SourceRef{
				SourceSystem: "git",
				ScopeID:      "repo-eshu",
				GenerationID: "generation-1",
				FactKey:      "scorecard:repo-eshu",
				URI:          "git://repo-eshu",
				RecordID:     "scorecard-record",
			},
			Payload: map[string]any{
				"score": 98,
				"state": "pass",
			},
		}},
	}
}

// commitListenerStubCommitter records the commit instead of persisting it.
// ClaimPlaneFenced reports true so the constructor accepts it beside a
// claim-plane control store, exactly like the production IngestionStore
// with its claim-plane fence.
type commitListenerStubCommitter struct {
	calls int
}

func (c *commitListenerStubCommitter) ClaimPlaneFenced() bool { return true }

func (c *commitListenerStubCommitter) CommitScopeGeneration(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error {
	return nil
}

func (c *commitListenerStubCommitter) CommitClaimedScopeGeneration(
	ctx context.Context,
	_ workflow.ClaimMutation,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	c.calls++
	for range factStream {
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// commitListenerStubPlane answers the fence probe the way a healthy claim
// plane does, so the test drives the production ClaimPlaneClient instead
// of a stub copy.
func commitListenerStubPlane(t *testing.T, heartbeats *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/workflow.v1/HeartbeatClaim", func(writer http.ResponseWriter, _ *http.Request) {
		heartbeats.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set(workflowv1.VersionHeader, "1")
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// TestCommitListenerDarkByDefault proves the listener stays dark when the
// operator sets none of its variables: no error, no listener.
func TestCommitListenerDarkByDefault(t *testing.T) {
	t.Parallel()

	cfg, err := commitListenerConfigFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("commitListenerConfigFromEnv() error = %v, want nil", err)
	}
	if cfg != nil {
		t.Fatalf("commitListenerConfigFromEnv() = %+v, want nil", cfg)
	}
}

// TestCommitListenerRequiresCompanions proves every companion variable is
// required once the listener address is set: a half-wired listener fails
// startup closed instead of serving an unfenced endpoint.
func TestCommitListenerRequiresCompanions(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"ESHU_COMMIT_LISTEN":          "0.0.0.0:8089",
		"ESHU_COMMIT_MANIFEST_PATH":   "/manifest.yaml",
		"ESHU_COMMIT_CLAIM_PLANE_URL": "http://claim-plane:8080",
		"ESHU_COMMIT_SOURCE_SYSTEM":   "git",
		"ESHU_COMMIT_SCOPE_KIND":      "repository",
		"ESHU_COMMIT_INSTANCE_ID":     "scorecard-instance",
	}
	for _, drop := range []string{
		"ESHU_COMMIT_MANIFEST_PATH",
		"ESHU_COMMIT_CLAIM_PLANE_URL",
		"ESHU_COMMIT_SOURCE_SYSTEM",
		"ESHU_COMMIT_SCOPE_KIND",
		"ESHU_COMMIT_INSTANCE_ID",
	} {
		env := map[string]string{}
		for k, v := range base {
			if k != drop {
				env[k] = v
			}
		}
		if _, err := commitListenerConfigFromEnv(func(k string) string { return env[k] }); err == nil {
			t.Errorf("commitListenerConfigFromEnv() without %s error = nil, want fail-closed", drop)
		}
	}
}

// TestCommitListenerRefusesHostname proves the listen address never takes a
// DNS name: the proxy dials a literal endpoint, and a name would resolve at
// dial time to something the deployment never reviewed.
func TestCommitListenerRefusesHostname(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"ESHU_COMMIT_LISTEN":          "eshu-api-commit:8089",
		"ESHU_COMMIT_MANIFEST_PATH":   "/manifest.yaml",
		"ESHU_COMMIT_CLAIM_PLANE_URL": "http://claim-plane:8080",
		"ESHU_COMMIT_SOURCE_SYSTEM":   "git",
		"ESHU_COMMIT_SCOPE_KIND":      "repository",
		"ESHU_COMMIT_INSTANCE_ID":     "scorecard-instance",
	}
	if _, err := commitListenerConfigFromEnv(func(k string) string { return env[k] }); err == nil {
		t.Fatal("commitListenerConfigFromEnv() with hostname error = nil, want refusal")
	}
}

// TestCommitListenerCommitClaimFactsEndToEnd is the #11064 regression: a
// CommitClaimFacts call succeeds end to end through the listener mux the
// binary serves on ESHU_COMMIT_LISTEN, admitted and fenced exactly like
// production. Before the fix no such mux existed, so the client dial met
// connection refused.
func TestCommitListenerCommitClaimFactsEndToEnd(t *testing.T) {
	t.Parallel()

	now := commitListenerObservedAt()
	var heartbeats atomic.Int64
	plane := commitListenerStubPlane(t, &heartbeats)
	committer := &commitListenerStubCommitter{}
	mux, err := newCommitListenerMux(
		workflow.NewClaimPlaneClient(plane.URL, plane.Client()),
		committer,
		commitListenerManifest(),
		nil,
		"git",
		scope.KindRepository,
		"scorecard-instance",
	)
	if err != nil {
		t.Fatalf("newCommitListenerMux() error = %v, want nil", err)
	}
	listener := httptest.NewServer(mux)
	defer listener.Close()

	rawResult, err := json.Marshal(commitListenerResult(now))
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	client := workflow.NewClaimPlaneClient(listener.URL, listener.Client())
	if err := client.CommitClaimFacts(
		context.Background(),
		commitListenerClaim(now),
		"commit-listener-key-1",
		rawResult,
	); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil", err)
	}
	if committer.calls != 1 {
		t.Fatalf("committer calls = %d, want 1", committer.calls)
	}
	if heartbeats.Load() != 1 {
		t.Fatalf("fence probes = %d, want 1", heartbeats.Load())
	}
}

// The stub stands in for the production IngestionStore with its
// claim-plane fence: it must satisfy the same commit interfaces the
// constructor and the commit path assert.
var (
	_ collector.Committer                 = (*commitListenerStubCommitter)(nil)
	_ collector.ClaimedCommitter          = (*commitListenerStubCommitter)(nil)
	_ collector.ClaimPlaneFencedCommitter = (*commitListenerStubCommitter)(nil)
)
