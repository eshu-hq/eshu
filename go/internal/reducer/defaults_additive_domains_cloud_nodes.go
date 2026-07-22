// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendCloudResourceNodeAdditiveDomains registers the cloud-resource node
// materialization domains and delegates to the per-provider node/relationship
// and security-group helpers, preserving the exact registration order of the
// original monolithic appendAdditiveDomainDefinitions: AWS resource nodes, the
// GCP and Azure resource/relationship domains, EC2 instance nodes, kubernetes
// workload nodes, kubernetes namespace nodes (issue #5434), and the
// security-group endpoint/reachability domains. Each
// inline registration is gated on the fact loader plus its node writer so the
// runtime never registers a domain without a durable publication path.
// Registration is keyed by Domain, so the append order is not runtime-observable.
func appendCloudResourceNodeAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.FactLoader != nil && handlers.CloudResourceNodeWriter != nil {
		awsResources := awsResourceMaterializationDomainDefinition()
		awsResources.Handler = AWSResourceMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.CloudResourceNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
			PresenceWriter: handlers.EndpointPresenceWriter,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, awsResources)
	}
	definitions = appendGCPResourceMaterializationDomain(definitions, handlers)
	definitions = appendGCPRelationshipMaterializationDomain(definitions, handlers)
	definitions = appendAzureResourceMaterializationDomain(definitions, handlers)
	definitions = appendAzureRelationshipMaterializationDomain(definitions, handlers)
	if handlers.FactLoader != nil && handlers.EC2InstanceNodeWriter != nil {
		ec2Instances := ec2InstanceNodeMaterializationDomainDefinition()
		ec2Instances.Handler = EC2InstanceNodeMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.EC2InstanceNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, ec2Instances)
	}
	if handlers.FactLoader != nil && handlers.KubernetesWorkloadNodeWriter != nil {
		kubernetesWorkloads := kubernetesWorkloadMaterializationDomainDefinition()
		kubernetesWorkloads.Handler = KubernetesWorkloadMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.KubernetesWorkloadNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
			PresenceWriter: handlers.EndpointPresenceWriter,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, kubernetesWorkloads)
	}
	if handlers.FactLoader != nil && handlers.KubernetesNamespaceNodeWriter != nil {
		kubernetesNamespaces := kubernetesNamespaceMaterializationDomainDefinition()
		kubernetesNamespaces.Handler = KubernetesNamespaceMaterializationHandler{
			FactLoader:  handlers.FactLoader,
			NodeWriter:  handlers.KubernetesNamespaceNodeWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, kubernetesNamespaces)
	}
	definitions = appendSecurityGroupEndpointDomain(definitions, handlers)
	definitions = appendSecurityGroupReachabilityDomains(definitions, handlers)
	return definitions
}
