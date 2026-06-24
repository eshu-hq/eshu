// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/truth"

// secretsIAMTrustChainDomainDefinition returns the additive definition for the
// secrets/IAM trust-chain read model. The domain writes durable reducer facts
// for exact, partial, stale, permission-hidden, and unsupported outcomes but
// deliberately does not declare graph writes or schema DDL.
func secretsIAMTrustChainDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainSecretsIAMTrustChain,
		Summary: "publish reducer-owned secrets/IAM trust-chain read models with explicit partial and unsupported states",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "secrets_iam_trust_chain",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}
