// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
)

// This file is the transitional compatibility surface for the secrets/IAM
// trust-chain and graph-projection family that moved to [secretsiam]
// (issue #6061). Reducer-root call sites and the external packages that name
// these types — cmd/reducer's wiring, internal/storage/postgres' readiness
// claim gate and evidence loader, and internal/replay's cost counting — keep
// their current spelling; each entry is deleted once its last caller has moved.

// SecretsIAMTrustChainState names how completely one identity trust chain
// resolved. See [secretsiam.SecretsIAMTrustChainState].
type SecretsIAMTrustChainState = secretsiam.SecretsIAMTrustChainState

const (
	// SecretsIAMTrustChainStateExact forwards to
	// [secretsiam.SecretsIAMTrustChainStateExact].
	SecretsIAMTrustChainStateExact = secretsiam.SecretsIAMTrustChainStateExact
	// SecretsIAMTrustChainStatePartial forwards to
	// [secretsiam.SecretsIAMTrustChainStatePartial].
	SecretsIAMTrustChainStatePartial = secretsiam.SecretsIAMTrustChainStatePartial
	// SecretsIAMTrustChainStateUnresolved forwards to
	// [secretsiam.SecretsIAMTrustChainStateUnresolved].
	SecretsIAMTrustChainStateUnresolved = secretsiam.SecretsIAMTrustChainStateUnresolved
	// SecretsIAMTrustChainStateStale forwards to
	// [secretsiam.SecretsIAMTrustChainStateStale].
	SecretsIAMTrustChainStateStale = secretsiam.SecretsIAMTrustChainStateStale
	// SecretsIAMTrustChainStatePermissionHidden forwards to
	// [secretsiam.SecretsIAMTrustChainStatePermissionHidden].
	SecretsIAMTrustChainStatePermissionHidden = secretsiam.SecretsIAMTrustChainStatePermissionHidden
	// SecretsIAMTrustChainStateUnsupported forwards to
	// [secretsiam.SecretsIAMTrustChainStateUnsupported].
	SecretsIAMTrustChainStateUnsupported = secretsiam.SecretsIAMTrustChainStateUnsupported
)

// SecretsIAMTrustChainReadModels carries the four reducer-owned secrets/IAM
// read models. See [secretsiam.SecretsIAMTrustChainReadModels].
type SecretsIAMTrustChainReadModels = secretsiam.SecretsIAMTrustChainReadModels

// SecretsIAMIdentityTrustChain is one resolved workload-to-identity chain.
// See [secretsiam.SecretsIAMIdentityTrustChain].
type SecretsIAMIdentityTrustChain = secretsiam.SecretsIAMIdentityTrustChain

// SecretsIAMPrivilegePostureObservation is one privilege-posture finding.
// See [secretsiam.SecretsIAMPrivilegePostureObservation].
type SecretsIAMPrivilegePostureObservation = secretsiam.SecretsIAMPrivilegePostureObservation

// SecretsIAMSecretAccessPath is one resolved identity-to-secret path.
// See [secretsiam.SecretsIAMSecretAccessPath].
type SecretsIAMSecretAccessPath = secretsiam.SecretsIAMSecretAccessPath

// SecretsIAMPostureGap records why a chain or path could not be completed.
// See [secretsiam.SecretsIAMPostureGap].
type SecretsIAMPostureGap = secretsiam.SecretsIAMPostureGap

// SecretsIAMTrustChainLoadStats summarizes the bounded evidence packet one
// intent loaded. See [secretsiam.SecretsIAMTrustChainLoadStats].
type SecretsIAMTrustChainLoadStats = secretsiam.SecretsIAMTrustChainLoadStats

// SecretsIAMTrustChainEvidenceLoader loads the bounded AWS IAM, Kubernetes and
// Vault source-fact packet. See
// [secretsiam.SecretsIAMTrustChainEvidenceLoader].
type SecretsIAMTrustChainEvidenceLoader = secretsiam.SecretsIAMTrustChainEvidenceLoader

// SecretsIAMTrustChainWrite carries read models for durable publication.
// See [secretsiam.SecretsIAMTrustChainWrite].
type SecretsIAMTrustChainWrite = secretsiam.SecretsIAMTrustChainWrite

// SecretsIAMTrustChainWriteResult summarizes durable publication. See
// [secretsiam.SecretsIAMTrustChainWriteResult].
type SecretsIAMTrustChainWriteResult = secretsiam.SecretsIAMTrustChainWriteResult

// SecretsIAMTrustChainWriter persists the four reducer-owned secrets/IAM fact
// kinds. See [secretsiam.SecretsIAMTrustChainWriter].
type SecretsIAMTrustChainWriter = secretsiam.SecretsIAMTrustChainWriter

// PostgresSecretsIAMTrustChainWriter is the Postgres-backed trust-chain
// writer. See [secretsiam.PostgresSecretsIAMTrustChainWriter].
type PostgresSecretsIAMTrustChainWriter = secretsiam.PostgresSecretsIAMTrustChainWriter

// SecretsIAMTrustChainHandler is the reducer handler for the trust-chain
// domain. See [secretsiam.SecretsIAMTrustChainHandler].
type SecretsIAMTrustChainHandler = secretsiam.SecretsIAMTrustChainHandler

// BuildSecretsIAMTrustChainReadModels forwards to
// [secretsiam.BuildSecretsIAMTrustChainReadModels].
func BuildSecretsIAMTrustChainReadModels(
	envelopes []facts.Envelope,
) (SecretsIAMTrustChainReadModels, []quarantinedFact, error) {
	return secretsiam.BuildSecretsIAMTrustChainReadModels(envelopes)
}

// SecretsIAMGraphWriter projects exact secrets/IAM read-model rows into the
// canonical graph. See [secretsiam.SecretsIAMGraphWriter].
type SecretsIAMGraphWriter = secretsiam.SecretsIAMGraphWriter

// SecretsIAMGraphProjectionHandler is the reducer handler for the secrets/IAM
// graph projection domain. See [secretsiam.SecretsIAMGraphProjectionHandler].
type SecretsIAMGraphProjectionHandler = secretsiam.SecretsIAMGraphProjectionHandler

// SecretsIAMGraphRows is the extracted node/edge row set one projection writes.
// See [secretsiam.SecretsIAMGraphRows].
type SecretsIAMGraphRows = secretsiam.SecretsIAMGraphRows

// SecretsIAMGraphTally counts what one extraction produced and skipped. See
// [secretsiam.SecretsIAMGraphTally].
type SecretsIAMGraphTally = secretsiam.SecretsIAMGraphTally

// SecretsIAMGraphEvidenceSource names the evidence source stamped on projected
// rows. See [secretsiam.SecretsIAMGraphEvidenceSource].
const SecretsIAMGraphEvidenceSource = secretsiam.SecretsIAMGraphEvidenceSource

// SecretsIAMEndpointNotReadyFailureClass is the retryable failure class the
// cross-scope readiness gate returns. The Postgres reducer queue matches this
// exact string when it decides to re-enqueue rather than dead-letter, so the
// literal is a storage contract, not just a Go identifier. See
// [secretsiam.SecretsIAMEndpointNotReadyFailureClass].
const SecretsIAMEndpointNotReadyFailureClass = secretsiam.SecretsIAMEndpointNotReadyFailureClass

// ExtractSecretsIAMGraphRows forwards to
// [secretsiam.ExtractSecretsIAMGraphRows].
func ExtractSecretsIAMGraphRows(envelopes []facts.Envelope) SecretsIAMGraphRows {
	return secretsiam.ExtractSecretsIAMGraphRows(envelopes)
}

// secretsIAMTrustChainDomainDefinition forwards to
// [secretsiam.TrustChainDomainDefinition].
func secretsIAMTrustChainDomainDefinition() DomainDefinition {
	return secretsiam.TrustChainDomainDefinition()
}

// secretsIAMGraphProjectionDomainDefinition forwards to
// [secretsiam.GraphProjectionDomainDefinition].
func secretsIAMGraphProjectionDomainDefinition() DomainDefinition {
	return secretsiam.GraphProjectionDomainDefinition()
}
