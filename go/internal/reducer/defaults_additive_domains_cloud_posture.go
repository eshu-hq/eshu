// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendCloudPostureEdgeAdditiveDomains registers the remaining cloud posture
// and IAM-action edge/node domains that read back committed graph state through
// the readiness lookup and prior-generation check: EC2 uses-profile edges, IAM
// instance-profile-role edges, EC2 block-device KMS posture nodes, S3 and EC2
// internet-exposure nodes, kubernetes correlation edges, IAM escalation edges,
// and IAM CAN_PERFORM edges. Each registration is gated on the fact loader plus
// its edge/node writer so the runtime never registers a domain without a durable
// publication path. Append order matches the original monolithic
// appendAdditiveDomainDefinitions; registration is keyed by Domain so order is
// not runtime-observable.
func appendCloudPostureEdgeAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.FactLoader != nil && handlers.EC2UsesProfileEdgeWriter != nil {
		ec2UsesProfile := ec2UsesProfileMaterializationDomainDefinition()
		ec2UsesProfile.Handler = EC2UsesProfileMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.EC2UsesProfileEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, ec2UsesProfile)
	}
	if handlers.FactLoader != nil && handlers.IAMInstanceProfileRoleEdgeWriter != nil {
		profileRole := iamInstanceProfileRoleMaterializationDomainDefinition()
		profileRole.Handler = IAMInstanceProfileRoleMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.IAMInstanceProfileRoleEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, profileRole)
	}
	if handlers.FactLoader != nil && handlers.EC2BlockDeviceKMSPostureNodeWriter != nil {
		ec2BlockDeviceKMS := ec2BlockDeviceKMSPostureMaterializationDomainDefinition()
		ec2BlockDeviceKMS.Handler = EC2BlockDeviceKMSPostureMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			NodeWriter:           handlers.EC2BlockDeviceKMSPostureNodeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, ec2BlockDeviceKMS)
	}
	if handlers.FactLoader != nil && handlers.S3InternetExposureNodeWriter != nil {
		s3Exposure := s3InternetExposureMaterializationDomainDefinition()
		s3Exposure.Handler = S3InternetExposureMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			NodeWriter:           handlers.S3InternetExposureNodeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, s3Exposure)
	}
	if handlers.FactLoader != nil && handlers.EC2InternetExposureNodeWriter != nil {
		ec2Exposure := ec2InternetExposureMaterializationDomainDefinition()
		ec2Exposure.Handler = EC2InternetExposureMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			NodeWriter:           handlers.EC2InternetExposureNodeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, ec2Exposure)
	}
	if handlers.FactLoader != nil && handlers.KubernetesCorrelationEdgeWriter != nil {
		kubernetesEdges := kubernetesCorrelationMaterializationDomainDefinition()
		kubernetesEdges.Handler = KubernetesCorrelationMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.KubernetesCorrelationEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, kubernetesEdges)
	}
	if handlers.FactLoader != nil && handlers.IAMEscalationEdgeWriter != nil {
		iamEscalation := iamEscalationMaterializationDomainDefinition()
		iamEscalation.Handler = IAMEscalationMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			Writer:               handlers.IAMEscalationEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, iamEscalation)
	}
	if handlers.FactLoader != nil && handlers.IAMCanPerformEdgeWriter != nil {
		iamCanPerform := iamCanPerformMaterializationDomainDefinition()
		iamCanPerform.Handler = IAMCanPerformMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			Writer:               handlers.IAMCanPerformEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, iamCanPerform)
	}
	return definitions
}
