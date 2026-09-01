// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestBuildProjectionRejectsUnsupportedServiceCatalogSchemaVersion pins the
// root schema-version gate for the service-catalog family: an unsupported
// service_catalog.entity schema_version fails projection before the
// servicecatalog builder ever sees the generation. It lives at root because
// validateFactSchemaVersion is root behavior, not the child builder's.
func TestBuildProjectionRejectsUnsupportedServiceCatalogSchemaVersion(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		SourceSystem: "service_catalog",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-service-catalog",
	}
	_, err := buildProjection(scopeValue, generation, []facts.Envelope{
		{
			FactID:        "service-catalog-entity",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      facts.ServiceCatalogEntityFactKind,
			SchemaVersion: "2099-01-01",
			Payload: map[string]any{
				"entity_ref": "component:default/checkout",
			},
		},
	})
	if err == nil {
		t.Fatal("buildProjection() error = nil, want unsupported schema_version error")
	}
}

// TestBuildProjectionRejectsUnsupportedSecretsIAMSchemaVersion pins the root
// schema-version gate for the secrets/IAM posture family: an unsupported
// k8s_service_account schema_version fails projection before the secretsiam
// builder ever sees the generation. It lives at root because
// validateFactSchemaVersion is root behavior, not the child builder's.
func TestBuildProjectionRejectsUnsupportedSecretsIAMSchemaVersion(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "kubernetes-manifests://repo-checkout/deploy",
		SourceSystem: "secrets_iam_posture",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-secrets-iam",
	}
	_, err := buildProjection(scopeValue, generation, []facts.Envelope{
		{
			FactID:        "secrets-iam-service-account-1",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      facts.KubernetesServiceAccountFactKind,
			SchemaVersion: "0.0.0",
			CollectorKind: "secrets_iam_posture",
			Payload: map[string]any{
				"service_account_join_key": "sha256:service-account",
			},
		},
	})
	if err == nil {
		t.Fatal("buildProjection() error = nil, want unsupported secrets/IAM schema version")
	}
}
