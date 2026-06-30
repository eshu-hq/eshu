// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendSecretsAndDriftAdditiveDomains registers the secrets/IAM trust and
// cloud-runtime drift domains: secrets-IAM trust chain, secrets-IAM graph
// projection, AWS cloud-runtime drift, multi-cloud runtime drift, and
// cloud-inventory admission. Each registration is gated on its explicit evidence
// loader / graph writer so the runtime never registers a domain without a
// durable publication path. Append order matches the original monolithic
// appendAdditiveDomainDefinitions; registration is keyed by Domain so order is
// not runtime-observable.
func appendSecretsAndDriftAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.SecretsIAMTrustChainEvidenceLoader != nil && handlers.SecretsIAMTrustChainWriter != nil {
		secretsIAM := secretsIAMTrustChainDomainDefinition()
		secretsIAM.Handler = SecretsIAMTrustChainHandler{
			EvidenceLoader: handlers.SecretsIAMTrustChainEvidenceLoader,
			Writer:         handlers.SecretsIAMTrustChainWriter,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, secretsIAM)
	}
	if handlers.FactLoader != nil && handlers.SecretsIAMGraphWriter != nil {
		secretsIAMGraph := secretsIAMGraphProjectionDomainDefinition()
		secretsIAMGraph.Handler = SecretsIAMGraphProjectionHandler{
			FactLoader:           handlers.FactLoader,
			Writer:               handlers.SecretsIAMGraphWriter,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			PresenceLookup:       handlers.EndpointPresenceLookup,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, secretsIAMGraph)
	}
	if handlers.AWSCloudRuntimeDriftEvidenceLoader != nil &&
		handlers.AWSCloudRuntimeDriftWriter != nil {
		awsRuntimeDrift := awsCloudRuntimeDriftDomainDefinition()
		awsRuntimeDrift.Handler = AWSCloudRuntimeDriftHandler{
			EvidenceLoader: handlers.AWSCloudRuntimeDriftEvidenceLoader,
			Writer:         handlers.AWSCloudRuntimeDriftWriter,
			Instruments:    handlers.Instruments,
			Logger:         handlers.AWSCloudRuntimeDriftLogger,
		}
		definitions = append(definitions, awsRuntimeDrift)
	}
	if handlers.MultiCloudRuntimeDriftEvidenceLoader != nil &&
		handlers.MultiCloudRuntimeDriftWriter != nil {
		multiCloudDrift := multiCloudRuntimeDriftDomainDefinition()
		multiCloudDrift.Handler = MultiCloudRuntimeDriftHandler{
			EvidenceLoader: handlers.MultiCloudRuntimeDriftEvidenceLoader,
			Writer:         handlers.MultiCloudRuntimeDriftWriter,
			Instruments:    handlers.Instruments,
			Logger:         handlers.MultiCloudRuntimeDriftLogger,
		}
		definitions = append(definitions, multiCloudDrift)
	}
	if handlers.CloudInventoryEvidenceLoader != nil && handlers.CloudInventoryAdmissionWriter != nil {
		cloudInventory := cloudInventoryAdmissionDomainDefinition()
		cloudInventory.Handler = CloudInventoryAdmissionHandler{
			EvidenceLoader:               handlers.CloudInventoryEvidenceLoader,
			Writer:                       handlers.CloudInventoryAdmissionWriter,
			GenerationCheck:              handlers.CloudInventoryGenerationCheck,
			TagEvidenceLoader:            handlers.CloudInventoryTagEvidenceLoader,
			IdentityPolicyEvidenceLoader: handlers.CloudInventoryIdentityPolicyEvidenceLoader,
			ResourceChangeEvidenceLoader: handlers.CloudInventoryResourceChangeEvidenceLoader,
			Instruments:                  handlers.Instruments,
			AdmissionDecisionWriter:      handlers.AdmissionDecisionWriter,
			AdmissionDecisionNow:         handlers.AdmissionDecisionNow,
		}
		definitions = append(definitions, cloudInventory)
	}
	return definitions
}
