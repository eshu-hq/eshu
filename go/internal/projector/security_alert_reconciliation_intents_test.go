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

// TestBuildProjectionQueuesSupplyChainImpactForProviderAlert previously
// called the root buildSupplyChainImpactReducerIntent directly. That builder
// moved into internal/projector/supplychainimpact with this extraction; the
// equivalent case (a security_alert.repository_alert fact producing the
// "provider security alert evidence observed" reason) is now
// TestBuildSupplyChainImpactReducerIntentReasonBySourceKind's "security
// alert" subtest in supplychainimpact/impact_intents_test.go. The provider
// alert fact still reaches buildProjection through
// TestBuildProjectionQueuesSecurityAlertReconciliationForProviderAlert above,
// which asserts the security_alert_reconciliation intent the same fact also
// produces; that dispatcher case is unchanged by this move.

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

func TestBuildProjectionRejectsStaleSecurityIntentFact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "repo://github/eshu-hq/eshu"}
	generation := scope.ScopeGeneration{ScopeID: scopeValue.ScopeID, GenerationID: "generation-current"}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{{
		FactID:       "alert-stale",
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-stale",
		FactKind:     facts.SecurityAlertRepositoryAlertFactKind,
	}})
	if err == nil {
		t.Fatal("buildProjection() error = nil, want stale generation rejection")
	}
	if len(projection.reducerIntents) != 0 {
		t.Fatalf("reducer intents = %#v, want none for rejected stale fact", projection.reducerIntents)
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
