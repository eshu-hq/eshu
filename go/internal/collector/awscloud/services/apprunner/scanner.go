// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apprunner

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS App Runner service, connection, autoscaling-configuration,
// observability-configuration, VPC-connector, VPC-ingress-connection, and
// relationship facts for one claimed account and region.
//
// The scanner is metadata-only. It never persists source repository
// credentials or runtime environment-variable values: only environment-
// variable names are kept, and secret references are recorded as Secrets
// Manager / SSM ARN reference edges. The scanner never mutates any App Runner
// resource.
type Scanner struct {
	Client Client
}

// Scan observes AWS App Runner resources through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("apprunner scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAppRunner:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAppRunner
	default:
		return nil, fmt.Errorf("apprunner scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	services, err := s.Client.ListServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list App Runner services: %w", err)
	}
	for _, service := range services {
		serviceEnvelopes, err := serviceEnvelopes(boundary, service)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, serviceEnvelopes...)
	}

	connections, err := s.Client.ListConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list App Runner connections: %w", err)
	}
	for _, connection := range connections {
		resource, err := awscloud.NewResourceEnvelope(connectionObservation(boundary, connection))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	autoScalingConfigurations, err := s.Client.ListAutoScalingConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list App Runner autoscaling configurations: %w", err)
	}
	for _, autoScalingConfiguration := range autoScalingConfigurations {
		resource, err := awscloud.NewResourceEnvelope(autoScalingConfigurationObservation(boundary, autoScalingConfiguration))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	observabilityConfigurations, err := s.Client.ListObservabilityConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list App Runner observability configurations: %w", err)
	}
	for _, observabilityConfiguration := range observabilityConfigurations {
		resource, err := awscloud.NewResourceEnvelope(observabilityConfigurationObservation(boundary, observabilityConfiguration))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	vpcConnectors, err := s.Client.ListVpcConnectors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list App Runner VPC connectors: %w", err)
	}
	for _, vpcConnector := range vpcConnectors {
		connectorEnvelopes, err := vpcConnectorEnvelopes(boundary, vpcConnector)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, connectorEnvelopes...)
	}

	vpcIngressConnections, err := s.Client.ListVpcIngressConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list App Runner VPC ingress connections: %w", err)
	}
	for _, vpcIngressConnection := range vpcIngressConnections {
		ingressEnvelopes, err := vpcIngressConnectionEnvelopes(boundary, vpcIngressConnection)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, ingressEnvelopes...)
	}

	return envelopes, nil
}

func serviceEnvelopes(boundary awscloud.Boundary, service Service) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(serviceObservation(boundary, service))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range serviceRelationships(boundary, service) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func vpcConnectorEnvelopes(boundary awscloud.Boundary, connector VpcConnector) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(vpcConnectorObservation(boundary, connector))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range vpcConnectorRelationships(boundary, connector) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func vpcIngressConnectionEnvelopes(boundary awscloud.Boundary, ingress VpcIngressConnection) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(vpcIngressConnectionObservation(boundary, ingress))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range vpcIngressConnectionRelationships(boundary, ingress) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func serviceObservation(boundary awscloud.Boundary, service Service) awscloud.ResourceObservation {
	serviceARN := strings.TrimSpace(service.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          serviceARN,
		ResourceID:   serviceARN,
		ResourceType: awscloud.ResourceTypeAppRunnerService,
		Name:         strings.TrimSpace(service.Name),
		State:        strings.TrimSpace(service.Status),
		Tags:         cloneStringMap(service.Tags),
		Attributes: map[string]any{
			"service_id":                      strings.TrimSpace(service.ID),
			"service_url":                     strings.TrimSpace(service.ServiceURL),
			"source_configuration_type":       strings.TrimSpace(service.SourceConfigurationType),
			"image_identifier":                strings.TrimSpace(service.ImageIdentifier),
			"image_repository_type":           strings.TrimSpace(service.ImageRepositoryType),
			"code_repository_url":             strings.TrimSpace(service.CodeRepositoryURL),
			"auto_deployments_enabled":        service.AutoDeploymentsEnabled,
			"connection_arn":                  strings.TrimSpace(service.ConnectionARN),
			"access_role_arn":                 strings.TrimSpace(service.AccessRoleARN),
			"instance_role_arn":               strings.TrimSpace(service.InstanceRoleARN),
			"kms_key":                         strings.TrimSpace(service.KMSKey),
			"vpc_connector_arn":               strings.TrimSpace(service.VpcConnectorARN),
			"egress_type":                     strings.TrimSpace(service.EgressType),
			"is_publicly_accessible":          service.IsPubliclyAccessible,
			"auto_scaling_configuration_arn":  strings.TrimSpace(service.AutoScalingConfigurationARN),
			"observability_enabled":           service.ObservabilityEnabled,
			"observability_configuration_arn": strings.TrimSpace(service.ObservabilityConfigurationARN),
			"health_check":                    healthCheckMap(service.HealthCheck),
			"environment_variable_names":      cloneStrings(service.EnvironmentVariableNames),
			"secret_reference_names":          secretReferenceNames(service.SecretReferences),
			"created_at":                      timeOrNil(service.CreatedAt),
			"updated_at":                      timeOrNil(service.UpdatedAt),
		},
		CorrelationAnchors: []string{serviceARN, strings.TrimSpace(service.Name), strings.TrimSpace(service.ServiceURL)},
		SourceRecordID:     serviceARN,
	}
}

func connectionObservation(boundary awscloud.Boundary, connection Connection) awscloud.ResourceObservation {
	connectionARN := strings.TrimSpace(connection.ARN)
	resourceID := firstNonEmpty(connectionARN, connection.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          connectionARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppRunnerConnection,
		Name:         strings.TrimSpace(connection.Name),
		State:        strings.TrimSpace(connection.Status),
		Attributes: map[string]any{
			"provider_type": strings.TrimSpace(connection.ProviderType),
			"status":        strings.TrimSpace(connection.Status),
			"created_at":    timeOrNil(connection.CreatedAt),
		},
		CorrelationAnchors: []string{connectionARN, strings.TrimSpace(connection.Name)},
		SourceRecordID:     resourceID,
	}
}

func autoScalingConfigurationObservation(
	boundary awscloud.Boundary,
	configuration AutoScalingConfiguration,
) awscloud.ResourceObservation {
	configurationARN := strings.TrimSpace(configuration.ARN)
	resourceID := firstNonEmpty(configurationARN, configuration.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          configurationARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppRunnerAutoScalingConfiguration,
		Name:         strings.TrimSpace(configuration.Name),
		State:        strings.TrimSpace(configuration.Status),
		Attributes: map[string]any{
			"revision":        configuration.Revision,
			"status":          strings.TrimSpace(configuration.Status),
			"is_default":      configuration.IsDefault,
			"latest":          configuration.Latest,
			"max_concurrency": configuration.MaxConcurrency,
			"max_size":        configuration.MaxSize,
			"min_size":        configuration.MinSize,
			"created_at":      timeOrNil(configuration.CreatedAt),
		},
		CorrelationAnchors: []string{configurationARN, strings.TrimSpace(configuration.Name)},
		SourceRecordID:     resourceID,
	}
}

func observabilityConfigurationObservation(
	boundary awscloud.Boundary,
	configuration ObservabilityConfiguration,
) awscloud.ResourceObservation {
	configurationARN := strings.TrimSpace(configuration.ARN)
	resourceID := firstNonEmpty(configurationARN, configuration.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          configurationARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppRunnerObservabilityConfiguration,
		Name:         strings.TrimSpace(configuration.Name),
		State:        strings.TrimSpace(configuration.Status),
		Attributes: map[string]any{
			"revision":     configuration.Revision,
			"status":       strings.TrimSpace(configuration.Status),
			"latest":       configuration.Latest,
			"trace_vendor": strings.TrimSpace(configuration.TraceVendor),
			"created_at":   timeOrNil(configuration.CreatedAt),
		},
		CorrelationAnchors: []string{configurationARN, strings.TrimSpace(configuration.Name)},
		SourceRecordID:     resourceID,
	}
}

func vpcConnectorObservation(boundary awscloud.Boundary, connector VpcConnector) awscloud.ResourceObservation {
	connectorARN := strings.TrimSpace(connector.ARN)
	resourceID := firstNonEmpty(connectorARN, connector.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          connectorARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppRunnerVpcConnector,
		Name:         strings.TrimSpace(connector.Name),
		State:        strings.TrimSpace(connector.Status),
		Attributes: map[string]any{
			"revision":           connector.Revision,
			"status":             strings.TrimSpace(connector.Status),
			"subnet_ids":         cloneStrings(connector.Subnets),
			"security_group_ids": cloneStrings(connector.SecurityGroups),
			"created_at":         timeOrNil(connector.CreatedAt),
		},
		CorrelationAnchors: []string{connectorARN, strings.TrimSpace(connector.Name)},
		SourceRecordID:     resourceID,
	}
}

func vpcIngressConnectionObservation(
	boundary awscloud.Boundary,
	ingress VpcIngressConnection,
) awscloud.ResourceObservation {
	ingressARN := strings.TrimSpace(ingress.ARN)
	resourceID := firstNonEmpty(ingressARN, ingress.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          ingressARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppRunnerVpcIngressConnection,
		Name:         strings.TrimSpace(ingress.Name),
		State:        strings.TrimSpace(ingress.Status),
		Attributes: map[string]any{
			"status":          strings.TrimSpace(ingress.Status),
			"domain_name":     strings.TrimSpace(ingress.DomainName),
			"service_arn":     strings.TrimSpace(ingress.ServiceARN),
			"vpc_endpoint_id": strings.TrimSpace(ingress.VpcEndpointID),
			"vpc_id":          strings.TrimSpace(ingress.VpcID),
			"created_at":      timeOrNil(ingress.CreatedAt),
		},
		CorrelationAnchors: []string{ingressARN, strings.TrimSpace(ingress.Name), strings.TrimSpace(ingress.ServiceARN)},
		SourceRecordID:     resourceID,
	}
}

func healthCheckMap(healthCheck HealthCheck) map[string]any {
	if healthCheck == (HealthCheck{}) {
		return nil
	}
	return map[string]any{
		"protocol":            strings.TrimSpace(healthCheck.Protocol),
		"path":                strings.TrimSpace(healthCheck.Path),
		"interval":            healthCheck.Interval,
		"timeout":             healthCheck.Timeout,
		"healthy_threshold":   healthCheck.HealthyThreshold,
		"unhealthy_threshold": healthCheck.UnhealthyThreshold,
	}
}

// secretReferenceNames records the environment-variable keys bound to secret
// references. The resolved secret values are never read; the secret ARN itself
// is carried only as a relationship edge.
func secretReferenceNames(secrets []SecretReference) []string {
	if len(secrets) == 0 {
		return nil
	}
	output := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if name := strings.TrimSpace(secret.Name); name != "" {
			output = append(output, name)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
