// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesSecurityAlertReconciliationForProviderAlert(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "repo://github/eshu-hq/eshu",
		SourceSystem: "security_alert",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-1",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{{
		FactID:        "alert-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion: facts.SecurityAlertSchemaVersionV1,
		SourceRef: facts.Ref{
			SourceSystem: "security_alert",
		},
		Payload: map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(42),
			"repository_id":         scopeValue.ScopeID,
			"package_id":            "npm://registry.npmjs.org/left-pad",
		},
	}})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireSecurityAlertReconciliationIntent(t, projection.reducerIntents)
	if got, want := intent.ScopeID, scopeValue.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := intent.Domain, reducer.DomainSecurityAlertReconciliation; got != want {
		t.Fatalf("Domain = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "alert-1"; got != want {
		t.Fatalf("FactID = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "security_alert"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesSupplyChainImpactForProviderAlert(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "repo://github/eshu-hq/eshu"}
	generation := scope.ScopeGeneration{ScopeID: scopeValue.ScopeID, GenerationID: "generation-1"}
	intent, ok := buildSupplyChainImpactReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{{
		FactID:        "alert-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion: facts.SecurityAlertSchemaVersionV1,
	}}))
	if !ok {
		t.Fatal("buildSupplyChainImpactReducerIntent() ok = false, want true for provider alert")
	}
	if got, want := intent.Reason, "provider security alert evidence observed"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesSecurityAlertReconciliationForPackageRegistryPackage(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "npm://registry.npmjs.org/serialize-javascript",
		SourceSystem: "package_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "package-generation-1",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{{
		FactID:        "package-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.PackageRegistryPackageFactKind,
		SchemaVersion: facts.PackageRegistryPackageSchemaVersion,
		SourceRef: facts.Ref{
			SourceSystem: "package_registry",
		},
		Payload: map[string]any{
			"package_id": "npm://registry.npmjs.org/serialize-javascript",
		},
	}})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireSecurityAlertReconciliationIntent(t, projection.reducerIntents)
	if got, want := intent.ScopeID, scopeValue.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "package-1"; got != want {
		t.Fatalf("FactID = %q, want package identity fact", got)
	}
	if got, want := intent.Reason, "package registry identity observed"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

func requireSecurityAlertReconciliationIntent(t *testing.T, intents []ReducerIntent) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == reducer.DomainSecurityAlertReconciliation {
			return intent
		}
	}
	t.Fatalf("security_alert_reconciliation intent missing from %#v", intents)
	return ReducerIntent{}
}
