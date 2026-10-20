// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// ProjectSecretsIAMGraphRows runs the full offline collector-fact chain the
// Ifá vacuity guards assert: collector facts -> trust-chain read models ->
// read-model envelopes -> graph-projection rows.
//
// It reuses the production seams at every step (BuildSecretsIAMTrustChainReadModels,
// the trust-chain writer's payload builders, ExtractSecretsIAMGraphRows), so
// the rows it returns are what the live trust-chain -> graph-projection chain
// would hand the writer for the same collector facts. Only the envelope
// transport is offline: scopeID and generationID stamp the read-model
// envelopes the way the trust-chain handler's intent would, while intent,
// source-system, and cause stay zero because the extractor never reads them.
//
// A malformed collector fact quarantines per-fact through the build's own
// partition and is returned for the caller to fail on; any other decode error
// is fatal and returned as the error, matching the build contract.
func ProjectSecretsIAMGraphRows(
	envelopes []facts.Envelope,
	scopeID string,
	generationID string,
) (SecretsIAMGraphRows, []factdecode.QuarantinedFact, error) {
	models, quarantined, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		return SecretsIAMGraphRows{}, quarantined, err
	}
	write := SecretsIAMTrustChainWrite{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Models:       models,
	}
	projected := make([]facts.Envelope, 0,
		len(models.IdentityTrustChains)+len(models.SecretAccessPaths))
	for _, chain := range models.IdentityTrustChains {
		projected = append(projected, facts.Envelope{
			ScopeID:      scopeID,
			GenerationID: generationID,
			FactKind:     secretsIAMIdentityTrustChainFactKind,
			Payload:      secretsIAMIdentityTrustChainPayload(write, chain),
		})
	}
	for _, path := range models.SecretAccessPaths {
		projected = append(projected, facts.Envelope{
			ScopeID:      scopeID,
			GenerationID: generationID,
			FactKind:     secretsIAMSecretAccessPathFactKind,
			Payload:      secretsIAMSecretAccessPathPayload(write, path),
		})
	}
	return ExtractSecretsIAMGraphRows(projected), quarantined, nil
}
