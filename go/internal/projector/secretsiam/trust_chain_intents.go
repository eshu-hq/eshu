// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildSecretsIAMTrustChainReducerIntent emits one reducer intent when any
// secrets/IAM posture source fact lands in a generation. Any fact whose kind
// the facts.SecretsIAMSchemaVersion registry recognizes (AWS IAM, GCP IAM,
// Kubernetes service accounts and RBAC, EKS identity, Vault, coverage
// warning) is a trigger; the anchor is the earliest such fact in original
// input order across every recognized kind, so a generation carrying several
// posture kinds still enqueues once with a stable FactID. Only envelope
// metadata is read — the payload is never decoded here, and schema-version
// admission stays with root projection. The reducer loader expands from that
// seed through redaction-safe active-generation join anchors. A generation
// with no secrets/IAM posture fact enqueues nothing.
func BuildSecretsIAMTrustChainReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstMatchingKindPredicate(
		func(kind string) bool {
			_, isSecretsIAMKind := facts.SecretsIAMSchemaVersion(kind)
			return isSecretsIAMKind
		},
		func(facts.Envelope) bool { return true },
	)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainSecretsIAMTrustChain,
		EntityKey:    "secrets_iam_trust_chain:" + scopeID,
		Reason:       "secrets/IAM source facts observed",
		FactID:       envelope.FactID,
		SourceSystem: sourceSystem(envelope),
	}, true
}

// sourceSystem labels the intent with the anchor fact's SourceRef source
// system, then its CollectorKind, each trimmed, then the literal
// "secrets_iam_posture". The literal third tier is what distinguishes this
// family from projectorintent.SourceSystem: a posture fact carrying neither
// envelope label is still attributed to the secrets/IAM posture collector
// instead of an empty string, so the fallback order must stay exactly as
// written.
func sourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	if value := strings.TrimSpace(envelope.CollectorKind); value != "" {
		return value
	}
	return "secrets_iam_posture"
}
