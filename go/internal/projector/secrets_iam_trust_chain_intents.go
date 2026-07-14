// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildSecretsIAMTrustChainReducerIntent emits one reducer intent when any
// secrets/IAM posture source fact lands in a generation. The reducer loader
// expands from that seed through redaction-safe active-generation join anchors.
func buildSecretsIAMTrustChainReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstMatchingKindPredicate(
		func(kind string) bool {
			_, isSecretsIAMKind := facts.SecretsIAMSchemaVersion(kind)
			return isSecretsIAMKind
		},
		func(facts.Envelope) bool { return true },
	)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainSecretsIAMTrustChain,
		EntityKey:    "secrets_iam_trust_chain:" + scopeValue.ScopeID,
		Reason:       "secrets/IAM source facts observed",
		FactID:       envelope.FactID,
		SourceSystem: secretsIAMSourceSystem(envelope),
	}, true
}

func secretsIAMSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	if value := strings.TrimSpace(envelope.CollectorKind); value != "" {
		return value
	}
	return "secrets_iam_posture"
}
