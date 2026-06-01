package reducer

// appendSecurityGroupEndpointDomain registers the additive security-group
// endpoint node materialization domain (issue #1135 PR2a) when its node writer
// and the fact loader are both wired. It is additive — registering the domain
// without a node writer would silently drop every aws_security_group_rule
// endpoint before it reached the graph — so the registration is gated on both
// dependencies, mirroring the AWS resource and Kubernetes workload node domains.
func appendSecurityGroupEndpointDomain(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.FactLoader == nil || handlers.SecurityGroupEndpointNodeWriter == nil {
		return definitions
	}
	endpoints := securityGroupCidrMaterializationDomainDefinition()
	endpoints.Handler = SecurityGroupCidrMaterializationHandler{
		FactLoader:     handlers.FactLoader,
		NodeWriter:     handlers.SecurityGroupEndpointNodeWriter,
		PhasePublisher: handlers.GraphProjectionPhasePublisher,
		Instruments:    handlers.Instruments,
	}
	return append(definitions, endpoints)
}
