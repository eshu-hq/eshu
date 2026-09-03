// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
)

// This file is the transitional compatibility surface for the secrets/IAM
// trust-chain and graph-projection family that moved to [secretsiam]
// (issue #6061). It carries only the names that still have a caller: the
// reducer root's own registration and handler wiring, plus cmd/reducer's
// writer construction, internal/storage/postgres' readiness claim gate and
// evidence loader, and internal/replay/costcounting's cost test. Everything
// else the family exports is reached as secretsiam.X, and each entry here is
// deleted once its last caller has moved.

// SecretsIAMTrustChainLoadStats summarizes the bounded evidence packet one
// intent loaded. internal/storage/postgres' evidence loader returns it. See
// [secretsiam.SecretsIAMTrustChainLoadStats].
type SecretsIAMTrustChainLoadStats = secretsiam.SecretsIAMTrustChainLoadStats

// SecretsIAMTrustChainEvidenceLoader loads the bounded AWS IAM, Kubernetes,
// GCP and Vault source-fact packet. See
// [secretsiam.SecretsIAMTrustChainEvidenceLoader].
type SecretsIAMTrustChainEvidenceLoader = secretsiam.SecretsIAMTrustChainEvidenceLoader

// SecretsIAMTrustChainWriter persists the four reducer-owned secrets/IAM fact
// kinds. See [secretsiam.SecretsIAMTrustChainWriter].
type SecretsIAMTrustChainWriter = secretsiam.SecretsIAMTrustChainWriter

// PostgresSecretsIAMTrustChainWriter is the Postgres-backed trust-chain
// writer cmd/reducer constructs. See
// [secretsiam.PostgresSecretsIAMTrustChainWriter].
type PostgresSecretsIAMTrustChainWriter = secretsiam.PostgresSecretsIAMTrustChainWriter

// SecretsIAMTrustChainHandler is the reducer handler for the trust-chain
// domain. See [secretsiam.SecretsIAMTrustChainHandler].
type SecretsIAMTrustChainHandler = secretsiam.SecretsIAMTrustChainHandler

// SecretsIAMGraphWriter projects exact secrets/IAM read-model rows into the
// canonical graph. A nil one keeps the projection domain unregistered, which
// is how live graph writes stay off until a deployment opts in. See
// [secretsiam.SecretsIAMGraphWriter].
type SecretsIAMGraphWriter = secretsiam.SecretsIAMGraphWriter

// SecretsIAMGraphProjectionHandler is the reducer handler for the secrets/IAM
// graph projection domain. See
// [secretsiam.SecretsIAMGraphProjectionHandler].
type SecretsIAMGraphProjectionHandler = secretsiam.SecretsIAMGraphProjectionHandler

// SecretsIAMEndpointNotReadyFailureClass is the retryable failure class the
// cross-scope readiness gate returns. internal/storage/postgres' reducer queue
// matches this exact string when it decides to re-enqueue rather than
// dead-letter, so the literal is a storage contract, not just a Go identifier.
// See [secretsiam.SecretsIAMEndpointNotReadyFailureClass].
const SecretsIAMEndpointNotReadyFailureClass = secretsiam.SecretsIAMEndpointNotReadyFailureClass

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
