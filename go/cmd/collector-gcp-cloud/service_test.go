// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/oauth2"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/gcpruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func smokeRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("gcp-cmd-smoke-redaction-key"))
	if err != nil {
		t.Fatalf("redact.NewKey: %v", err)
	}
	return key
}

// TestBuildSourceWiresFixtureGCPSource proves the binary constructs a
// fixture-backed gcpruntime.Source from the declarative config without any live
// Google Cloud call.
func TestBuildSourceWiresFixtureGCPSource(t *testing.T) {
	t.Parallel()

	source, runtimeCfg, _, err := buildSource(
		"testdata/config.json",
		smokeRedactionKey(t),
		metricnoop.NewMeterProvider().Meter("test"),
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildSource() error = %v, want nil", err)
	}
	if runtimeCfg.CollectorInstanceID != "gcp-instance-smoke" {
		t.Fatalf("collector instance id = %q, want gcp-instance-smoke", runtimeCfg.CollectorInstanceID)
	}
	if got, want := runtimeCfg.PollInterval, 15*time.Minute; got != want {
		t.Fatalf("poll interval = %v, want %v", got, want)
	}
	if _, ok := source.Provider.(*gcpruntime.FixturePageProvider); !ok {
		t.Fatalf("provider type = %T, want *gcpruntime.FixturePageProvider", source.Provider)
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("source.Next() ok=%v err=%v", ok, err)
	}
	count := 0
	for env := range collected.Facts {
		if env.FactKind == facts.GCPCloudResourceFactKind {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("resource fact count = %d, want 3", count)
	}
}

// TestStatusCommitterWritesFactsAndRecordsClaim proves the GCP status committer
// forwards facts to the inner committer and records a claim outcome.
func TestStatusCommitterWritesFactsAndRecordsClaim(t *testing.T) {
	t.Parallel()

	metrics, err := gcpcloud.NewMetrics(metricnoop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("gcpcloud.NewMetrics: %v", err)
	}
	inner := &recordingCommitter{}
	committer := newGCPStatusCommitter(inner, metrics)

	scopeValue := scope.IngestionScope{
		ScopeID:       "gcp:project:my-project:mixed:resource:global",
		SourceSystem:  gcpcloud.CollectorKind,
		ScopeKind:     scope.KindAccount,
		CollectorKind: scope.CollectorGCP,
		PartitionKey:  "gcp:project:my-project:mixed:resource:global",
	}
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	generation := scope.ScopeGeneration{
		GenerationID: "gen-1",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	stream := make(chan facts.Envelope, 1)
	stream <- facts.Envelope{FactKind: facts.GCPCloudResourceFactKind}
	close(stream)

	if err := committer.CommitScopeGeneration(context.Background(), scopeValue, generation, stream); err != nil {
		t.Fatalf("CommitScopeGeneration: %v", err)
	}
	if inner.commits != 1 {
		t.Fatalf("inner commits = %d, want 1", inner.commits)
	}
	if inner.facts != 1 {
		t.Fatalf("inner facts = %d, want 1", inner.facts)
	}
	if inner.scopeID != scopeValue.ScopeID {
		t.Fatalf("inner scope id = %q, want %q", inner.scopeID, scopeValue.ScopeID)
	}
}

func TestBuildClaimedServiceWiresLiveClaimRuntime(t *testing.T) {
	oldFactory := newGCPADCLiveClient
	t.Cleanup(func() {
		newGCPADCLiveClient = oldFactory
	})
	var gotCredentialRef string
	newGCPADCLiveClient = func(_ context.Context, credentialRef string) (gcpruntime.LiveClient, error) {
		gotCredentialRef = credentialRef
		return gcpruntime.LiveClient{
			CredentialRef: credentialRef,
			TokenSource:   oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"}),
		}, nil
	}

	service, err := buildClaimedService(
		context.Background(),
		postgres.SQLDB{},
		smokeRedactionKey(t),
		claimedRuntimeEnv,
		noop.NewTracerProvider().Tracer("test"),
		metricnoop.NewMeterProvider().Meter("test"),
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if got, want := service.CollectorInstanceID, "gcp-primary"; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := gotCredentialRef, "readonly-ref"; got != want {
		t.Fatalf("credential ref = %q, want %q", got, want)
	}
	source, ok := service.Source.(*gcpruntime.Source)
	if !ok {
		t.Fatalf("Source type = %T, want *gcpruntime.Source", service.Source)
	}
	if _, ok := source.Provider.(gcpruntime.LiveClient); !ok {
		t.Fatalf("Provider type = %T, want gcpruntime.LiveClient", source.Provider)
	}
}

func claimedRuntimeEnv(key string) string {
	switch key {
	case envCollectorInstances:
		return `[{
			"instance_id": "gcp-primary",
			"collector_kind": "gcp",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"live_collection_enabled": true,
				"scopes": [{
					"enabled": true,
					"parent_scope_kind": "project",
					"parent_scope_id": "project-alpha",
					"credential_ref": "readonly-ref"
				}]
			}
		}]`
	case envCollectorInstanceID:
		return "gcp-primary"
	case envOwnerID:
		return "pod-1"
	default:
		return ""
	}
}

type recordingCommitter struct {
	commits int
	facts   int
	scopeID string
}

func (c *recordingCommitter) CommitScopeGeneration(
	_ context.Context,
	scopeValue scope.IngestionScope,
	_ scope.ScopeGeneration,
	stream <-chan facts.Envelope,
) error {
	c.commits++
	c.scopeID = scopeValue.ScopeID
	for range stream {
		c.facts++
	}
	return nil
}
