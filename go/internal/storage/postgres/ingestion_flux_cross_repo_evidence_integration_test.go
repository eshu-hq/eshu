// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestIngestionStoreCommitScopeGenerationLinksFluxCrossRepoEvidenceAndEmitsMetric
// drives the real IngestionStore.CommitScopeGeneration ->
// relationships.DiscoverEvidenceWithStats -> recordFluxCrossRepoURLResolutionStats
// path end to end (issue #5483 C2): a "repo-config" repository commits a file
// fact whose parsed_file_data.flux_git_repositories[].url normalizes to
// exactly the RemoteURL the catalog already carries for "repo-deploy". The
// commit must both persist a DEPLOYS_FROM evidence fact (proven indirectly
// through no error and the evidence log path) and increment
// eshu_dp_flux_cross_repo_url_resolution_total{outcome="linked"} exactly once.
func TestIngestionStoreCommitScopeGenerationLinksFluxCrossRepoEvidenceAndEmitsMetric(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	now := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			// The target repository is already a known catalog entry with a
			// normalized RemoteURL, mirroring a repository the ingester indexed
			// in an earlier generation.
			[]byte(`{"graph_id":"repo-deploy","remote_url":"https://github.com/myorg/payments-deploy.git"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.Instruments = instruments

	fluxEnvelope := facts.Envelope{
		FactID:        "fact-flux-git-repository",
		ScopeID:       "scope-config",
		GenerationID:  "gen-config-1",
		FactKind:      "file",
		StableFactKey: "file:fact-flux-git-repository",
		ObservedAt:    now.Add(-time.Minute),
		Payload: map[string]any{
			"repo_id":       "repo-config",
			"relative_path": "clusters/prod/git-repository.yaml",
			"parsed_file_data": map[string]any{
				"flux_git_repositories": []any{
					map[string]any{
						"name": "app-source",
						"url":  "https://github.com/myorg/payments-deploy.git",
					},
				},
			},
		},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: "fact-flux-git-repository"},
	}

	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-config", "repo-config"),
		catalogTestGeneration("scope-config", "gen-config-1", now),
		testFactChannel([]facts.Envelope{fluxEnvelope}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	var linkedCount int64 = -1
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != "eshu_dp_flux_cross_repo_url_resolution_total" {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Sum[int64]", metricRecord.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome, ok := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				if !ok || outcome.AsString() != "linked" {
					continue
				}
				linkedCount = dp.Value
			}
		}
	}
	if linkedCount != 1 {
		t.Fatalf("eshu_dp_flux_cross_repo_url_resolution_total{outcome=linked} = %d, want 1", linkedCount)
	}
}
