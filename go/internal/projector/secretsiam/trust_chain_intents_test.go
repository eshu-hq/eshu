// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "kubernetes-manifests://repo-checkout/deploy"
	testGenerationID = "generation-secrets-iam"
)

func postureEnvelope(factID, factKind, sourceSystem, collectorKind string) facts.Envelope {
	version, _ := facts.SecretsIAMSchemaVersion(factKind)
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      factKind,
		SchemaVersion: version,
		CollectorKind: collectorKind,
		SourceRef:     facts.Ref{SourceSystem: sourceSystem},
		Payload:       map[string]any{"service_account_join_key": "sha256:service-account"},
	}
}

// TestBuildSecretsIAMTrustChainReducerIntent proves the builder anchors to the
// earliest secrets/IAM posture fact in original input order regardless of
// which posture kind it is, that every registry-recognized posture kind is a
// trigger on its own, that the source-system label falls back from
// SourceRef.SourceSystem to CollectorKind to the literal secrets_iam_posture
// in that order, and that a generation with no posture fact enqueues nothing.
func TestBuildSecretsIAMTrustChainReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues once from the earliest posture fact across kinds", func(t *testing.T) {
		t.Parallel()
		// The trust policy is placed before the service account on purpose:
		// the trigger is any recognized posture kind, so input order picks
		// the anchor, not a per-kind priority.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: "code_symbol_reference"},
			postureEnvelope("secrets-iam-trust-policy", facts.AWSIAMTrustPolicyFactKind, "secrets_iam_posture", ""),
			postureEnvelope("secrets-iam-service-account", facts.KubernetesServiceAccountFactKind, "secrets_iam_posture", ""),
		})
		got, ok := BuildSecretsIAMTrustChainReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainSecretsIAMTrustChain,
			EntityKey: "secrets_iam_trust_chain:" + testScopeID,
			Reason:    "secrets/IAM source facts observed",
			FactID:    "secrets-iam-trust-policy", SourceSystem: "secrets_iam_posture",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("every registry-recognized posture kind triggers on its own", func(t *testing.T) {
		t.Parallel()
		for _, kind := range facts.SecretsIAMFactKinds() {
			lookup := projectorintent.NewFactLookup([]facts.Envelope{
				{FactID: "decoy-1", FactKind: "code_symbol_reference"},
				postureEnvelope("posture-"+kind, kind, "secrets_iam_posture", ""),
			})
			got, ok := BuildSecretsIAMTrustChainReducerIntent(testScopeID, testGenerationID, lookup)
			if !ok {
				t.Fatalf("kind %q: ok = false, want true", kind)
			}
			if got.FactID != "posture-"+kind {
				t.Fatalf("kind %q: FactID = %q, want %q", kind, got.FactID, "posture-"+kind)
			}
		}
	})

	t.Run("falls back to CollectorKind when SourceRef is blank", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("secrets-iam-service-account", facts.KubernetesServiceAccountFactKind, "  ", "vault_collector"),
		})
		got, ok := BuildSecretsIAMTrustChainReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "vault_collector" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "vault_collector")
		}
	})

	t.Run("falls back to the secrets_iam_posture literal when the envelope carries no label", func(t *testing.T) {
		t.Parallel()
		// This literal third tier is the one projectorintent.SourceSystem
		// lacks; it is why the builder keeps its own helper.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("secrets-iam-service-account", facts.KubernetesServiceAccountFactKind, "", "  "),
		})
		got, ok := BuildSecretsIAMTrustChainReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "secrets_iam_posture" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "secrets_iam_posture")
		}
	})

	t.Run("does not queue without a secrets/IAM posture fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: "code_symbol_reference"},
			{FactID: "decoy-2", FactKind: facts.PackageRegistryPackageFactKind},
		})
		got, ok := BuildSecretsIAMTrustChainReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) without a posture fact, want zero intent and false", got, ok)
		}
	})
}
