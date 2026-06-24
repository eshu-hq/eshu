// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func secretsIAMFactEnvelope(factID, scopeID, generationID, kind string) facts.Envelope {
	version, _ := facts.SecretsIAMSchemaVersion(kind)
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      kind,
		SchemaVersion: version,
		CollectorKind: "secrets_iam_posture",
		ObservedAt:    time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "secrets_iam_posture",
		},
		Payload: map[string]any{
			"scope_id":                         scopeID,
			"generation_id":                    generationID,
			"provider":                         "kubernetes",
			"service_account_join_key":         "sha256:service-account",
			"web_identity_subject_fingerprint": "sha256:subject",
		},
	}
}

func TestBuildProjectionQueuesSecretsIAMTrustChainForSourceFacts(t *testing.T) {
	t.Parallel()

	scopeValue, generation := observabilityCoverageScopeAndGeneration()
	envelopes := []facts.Envelope{
		secretsIAMFactEnvelope(
			"secrets-iam-service-account-1",
			scopeValue.ScopeID,
			generation.GenerationID,
			facts.KubernetesServiceAccountFactKind,
		),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainSecretsIAMTrustChain)
	if got, want := intent.EntityKey, "secrets_iam_trust_chain:"+scopeValue.ScopeID; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "secrets-iam-service-account-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first secrets/IAM source fact", got)
	}
	if got, want := intent.SourceSystem, "secrets_iam_posture"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionRejectsUnsupportedSecretsIAMSchemaVersion(t *testing.T) {
	t.Parallel()

	scopeValue, generation := observabilityCoverageScopeAndGeneration()
	fact := secretsIAMFactEnvelope(
		"secrets-iam-service-account-1",
		scopeValue.ScopeID,
		generation.GenerationID,
		facts.KubernetesServiceAccountFactKind,
	)
	fact.SchemaVersion = "0.0.0"

	if _, err := buildProjection(scopeValue, generation, []facts.Envelope{fact}); err == nil {
		t.Fatal("buildProjection() error = nil, want unsupported secrets/IAM schema version")
	}
}
