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

// TestProjectEnforcesCentralSchemaVersionForPreviouslyUngatedFamily proves the
// central admission gate validates schema versions for a fact family that had no
// per-family projector validator before #3211. azure_cloud_resource is admitted
// at its supported version and rejected for an older major, a future major, and
// a blank version. It lives at root because validateFactSchemaVersion is root
// behavior, not the cloudinventory child builder's.
func TestProjectEnforcesCentralSchemaVersionForPreviouslyUngatedFamily(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "azure:acct:demo",
		ScopeKind:    scope.ScopeKind("azure_cloud"),
		SourceSystem: "azure",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "azure-generation-1",
		Status:       scope.GenerationStatusPending,
	}
	kind := facts.AzureCloudResourceFactKind
	envelope := func(factID, schemaVersion string) facts.Envelope {
		return facts.Envelope{
			FactID:        factID,
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      kind,
			SchemaVersion: schemaVersion,
			CollectorKind: "azure",
			SourceRef:     facts.Ref{SourceSystem: "azure"},
		}
	}

	supportedVersion, registered := facts.SchemaVersion(kind)
	if !registered {
		t.Fatalf("facts.SchemaVersion(%q) is not registered; the supported-version case would otherwise assert against an empty version and pass for the wrong reason", kind)
	}
	if _, err := buildProjection(scopeValue, generation, []facts.Envelope{
		envelope("fact-current", supportedVersion),
	}); err != nil {
		t.Fatalf("current schema version rejected: %v", err)
	}

	for _, tc := range []struct{ name, version string }{
		{"older major", "0.9.0"},
		{"future major", "2.0.0"},
		{"blank", ""},
	} {
		if _, err := buildProjection(scopeValue, generation, []facts.Envelope{
			envelope("fact-bad", tc.version),
		}); err == nil {
			t.Fatalf("%s schema version %q admitted for previously-ungated family, want rejected", tc.name, tc.version)
		}
	}
}

// TestBuildProjectionRejectsUnsupportedObservabilitySchemaVersion pins the
// root schema-version gate for the observability family: an unsupported
// observability source-fact schema_version fails projection before the
// observabilitycoverage builder ever sees the generation. It lives at root
// because validateFactSchemaVersion is root behavior, not the child
// builder's. Relocated from the pre-extraction
// observability_coverage_correlation_intents_test.go.
func TestBuildProjectionRejectsUnsupportedObservabilitySchemaVersion(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:123456789012:us-east-1:lambda",
		ScopeKind:    scope.ScopeKind("aws_cloud"),
		SourceSystem: "aws",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "aws-generation-1",
	}
	_, err := buildProjection(scopeValue, generation, []facts.Envelope{
		{
			FactID:        "observability-dashboard-1",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      facts.ObservabilityDeclaredDashboardFactKind,
			SchemaVersion: "0.0.0",
			CollectorKind: "git",
			Payload: map[string]any{
				"provider":      "grafana",
				"source_class":  "declared",
				"dashboard_uid": "checkout-latency",
			},
		},
	})
	if err == nil {
		t.Fatal("buildProjection() error = nil, want unsupported observability schema version")
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
