// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestIngestionStoreCommitScopeGenerationEmitsApplicationSetTemplateSourceMetric
// drives the real IngestionStore.CommitScopeGeneration ->
// relationships.DiscoverEvidenceWithStats ->
// recordArgoCDApplicationSetTemplateSourceStats path (issue #7767): an
// ApplicationSet in repo-gitops reads its config from repo-app and also names
// repo-app as the template source. The commit must increment
// eshu_dp_argocd_applicationset_template_source_total{outcome=
// "template_source_self_reference"} exactly once, and nothing else.
func TestIngestionStoreCommitScopeGenerationEmitsApplicationSetTemplateSourceMetric(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	db := &countingCatalogDB{
		catalogPayloads: [][]byte{
			[]byte(`{"graph_id":"repo-app","name":"app-repo"}`),
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.Instruments = instruments

	appSetEnvelope := facts.Envelope{
		FactID:        "fact-applicationset",
		ScopeID:       "scope-gitops",
		GenerationID:  "gen-gitops-1",
		FactKind:      "file",
		StableFactKey: "file:fact-applicationset",
		ObservedAt:    now.Add(-time.Minute),
		Payload: map[string]any{
			"repo_id":       "repo-gitops",
			"relative_path": "applicationsets/svc.yaml",
			"artifact_type": "argocd",
			"parsed_file_data": map[string]any{
				"argocd_applicationsets": []any{
					map[string]any{
						"name":                   "svc",
						"generator_source_repos": "https://github.com/myorg/app-repo",
						"generator_source_paths": "argocd/svc/overlays/*/config.yaml",
						"template_source_repos":  "https://github.com/myorg/app-repo",
						"template_source_paths":  "deploy",
						"dest_server":            "{{ .server }}",
					},
				},
			},
		},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: "fact-applicationset"},
	}

	if err := store.CommitScopeGeneration(
		context.Background(),
		catalogTestScope("scope-gitops", "repo-gitops"),
		catalogTestGeneration("scope-gitops", "gen-gitops-1", now),
		testFactChannel([]facts.Envelope{appSetEnvelope}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	got := collectApplicationSetTemplateSourcePoints(t, reader)
	want := map[string]int64{
		"template_source_self_reference": 1,
		"skipped_templated_destination":  1,
	}
	if len(got) != len(want) {
		t.Fatalf("data points = %#v, want %#v", got, want)
	}
	for outcome, value := range want {
		if got[outcome] != value {
			t.Fatalf("outcome %q = %d, want %d (all: %#v)", outcome, got[outcome], value, got)
		}
	}
}
