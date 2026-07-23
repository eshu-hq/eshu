// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendCrossplaneAdditiveDomains registers the Crossplane Claim -> XRD
// SATISFIED_BY edge domain (issue #5347). Gated on the fact loader plus the
// edge writer so the runtime never registers the domain without a durable
// publication path, mirroring the other additive domain helpers.
func appendCrossplaneAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.FactLoader != nil && handlers.CrossplaneSatisfiedByEdgeWriter != nil {
		crossplaneSatisfiedBy := crossplaneSatisfiedByMaterializationDomainDefinition()
		crossplaneSatisfiedBy.Handler = CrossplaneSatisfiedByMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.CrossplaneSatisfiedByEdgeWriter,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
			RedriveTargetLedger:  handlers.CrossplaneRedriveTargetLedger,
		}
		definitions = append(definitions, crossplaneSatisfiedBy)
	}
	return definitions
}
