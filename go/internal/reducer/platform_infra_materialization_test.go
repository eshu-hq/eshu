// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func platformInfraTerraformEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-infra-eks",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo:infra-eks",
				"repo_name":     "infra-eks",
				"source_run_id": "run-1",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-infra-eks",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"repo_id": "repo:infra-eks",
				"terraform_resources": []any{
					map[string]any{
						"name":          "aws_eks_cluster.prod",
						"resource_type": "aws_eks_cluster",
						"resource_name": "prod",
					},
				},
				"terraform_modules": []any{
					map[string]any{
						"name":   "cluster",
						"source": "terraform-aws-modules/eks/aws",
					},
				},
			},
		},
	}
}

func TestPlatformInfraMaterializationHandler_WritesProvisionsPlatform(t *testing.T) {
	t.Parallel()

	executor := &recordingCypherExecutor{}
	handler := PlatformInfraMaterializationHandler{
		FactLoader:                 &stubFactLoader{envelopes: platformInfraTerraformEnvelopes()},
		InfrastructureMaterializer: NewInfrastructurePlatformMaterializer(executor),
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		Domain:       DomainPlatformInfraMaterialization,
		ScopeID:      "scope-infra-eks",
		GenerationID: "gen-1",
		EnqueuedAt:   time.Unix(1700000000, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Domain != DomainPlatformInfraMaterialization {
		t.Fatalf("result.Domain = %q, want %q", result.Domain, DomainPlatformInfraMaterialization)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(executor.calls))
	}
	if !strings.Contains(executor.calls[0].cypher, "PROVISIONS_PLATFORM") {
		t.Fatalf("cypher missing PROVISIONS_PLATFORM: %s", executor.calls[0].cypher)
	}
	rowsParam, ok := executor.calls[0].params["rows"].([]map[string]any)
	if !ok || len(rowsParam) != 1 {
		t.Fatalf("rows param = %T len mismatch", executor.calls[0].params["rows"])
	}
	if rowsParam[0]["repo_id"] != "repo:infra-eks" {
		t.Fatalf("repo_id = %v, want repo:infra-eks", rowsParam[0]["repo_id"])
	}
	if rowsParam[0]["platform_kind"] != "eks" {
		t.Fatalf("platform_kind = %v, want eks", rowsParam[0]["platform_kind"])
	}
}

func TestPlatformInfraMaterializationHandler_RejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := PlatformInfraMaterializationHandler{
		FactLoader:                 &stubFactLoader{},
		InfrastructureMaterializer: NewInfrastructurePlatformMaterializer(&recordingCypherExecutor{}),
	}
	if _, err := handler.Handle(context.Background(), Intent{Domain: DomainCodeCallMaterialization}); err == nil {
		t.Fatal("expected error for wrong domain, got nil")
	}
}

func TestPlatformInfraMaterializationHandler_NoTerraformSignals(t *testing.T) {
	t.Parallel()

	executor := &recordingCypherExecutor{}
	handler := PlatformInfraMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo-1",
				ScopeID:  "scope-plain",
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":       "repo:plain",
					"repo_name":     "plain",
					"source_run_id": "run-1",
				},
			},
		}},
		InfrastructureMaterializer: NewInfrastructurePlatformMaterializer(executor),
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		Domain:       DomainPlatformInfraMaterialization,
		ScopeID:      "scope-plain",
		GenerationID: "gen-1",
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for a repo with no terraform signals", result.CanonicalWrites)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0 (no platform write)", len(executor.calls))
	}
}
