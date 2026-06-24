// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapprunnertypes "github.com/aws/aws-sdk-go-v2/service/apprunner/types"

	apprunnerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apprunner"
)

// mapService converts an App Runner DescribeService payload into the scanner-
// owned record. Runtime environment-variable VALUES are never read: only the
// variable names survive, and secret references carry the Secrets Manager / SSM
// ARN reference only.
func mapService(service *awsapprunnertypes.Service, tags map[string]string) apprunnerservice.Service {
	mapped := apprunnerservice.Service{
		ARN:        strings.TrimSpace(aws.ToString(service.ServiceArn)),
		ID:         strings.TrimSpace(aws.ToString(service.ServiceId)),
		Name:       strings.TrimSpace(aws.ToString(service.ServiceName)),
		Status:     strings.TrimSpace(string(service.Status)),
		ServiceURL: strings.TrimSpace(aws.ToString(service.ServiceUrl)),
		Tags:       tags,
	}
	if service.CreatedAt != nil {
		mapped.CreatedAt = *service.CreatedAt
	}
	if service.UpdatedAt != nil {
		mapped.UpdatedAt = *service.UpdatedAt
	}
	mapSourceConfiguration(&mapped, service.SourceConfiguration)
	mapInstanceConfiguration(&mapped, service.InstanceConfiguration)
	mapNetworkConfiguration(&mapped, service.NetworkConfiguration)
	if service.EncryptionConfiguration != nil {
		mapped.KMSKey = strings.TrimSpace(aws.ToString(service.EncryptionConfiguration.KmsKey))
	}
	if summary := service.AutoScalingConfigurationSummary; summary != nil {
		mapped.AutoScalingConfigurationARN = strings.TrimSpace(aws.ToString(summary.AutoScalingConfigurationArn))
	}
	if obs := service.ObservabilityConfiguration; obs != nil {
		mapped.ObservabilityEnabled = obs.ObservabilityEnabled
		mapped.ObservabilityConfigurationARN = strings.TrimSpace(aws.ToString(obs.ObservabilityConfigurationArn))
	}
	mapped.HealthCheck = mapHealthCheck(service.HealthCheckConfiguration)
	return mapped
}

func mapSourceConfiguration(mapped *apprunnerservice.Service, source *awsapprunnertypes.SourceConfiguration) {
	if source == nil {
		return
	}
	if auth := source.AuthenticationConfiguration; auth != nil {
		mapped.AccessRoleARN = strings.TrimSpace(aws.ToString(auth.AccessRoleArn))
		mapped.ConnectionARN = strings.TrimSpace(aws.ToString(auth.ConnectionArn))
	}
	mapped.AutoDeploymentsEnabled = aws.ToBool(source.AutoDeploymentsEnabled)
	switch {
	case source.ImageRepository != nil:
		mapped.SourceConfigurationType = "IMAGE"
		mapped.ImageIdentifier = strings.TrimSpace(aws.ToString(source.ImageRepository.ImageIdentifier))
		mapped.ImageRepositoryType = strings.TrimSpace(string(source.ImageRepository.ImageRepositoryType))
		if config := source.ImageRepository.ImageConfiguration; config != nil {
			mapped.EnvironmentVariableNames = sortedKeys(config.RuntimeEnvironmentVariables)
			mapped.SecretReferences = secretReferences(config.RuntimeEnvironmentSecrets)
		}
	case source.CodeRepository != nil:
		mapped.SourceConfigurationType = "REPOSITORY"
		mapped.CodeRepositoryURL = strings.TrimSpace(aws.ToString(source.CodeRepository.RepositoryUrl))
		if config := source.CodeRepository.CodeConfiguration; config != nil && config.CodeConfigurationValues != nil {
			values := config.CodeConfigurationValues
			mapped.EnvironmentVariableNames = sortedKeys(values.RuntimeEnvironmentVariables)
			mapped.SecretReferences = secretReferences(values.RuntimeEnvironmentSecrets)
		}
	}
}

func mapInstanceConfiguration(mapped *apprunnerservice.Service, instance *awsapprunnertypes.InstanceConfiguration) {
	if instance == nil {
		return
	}
	mapped.InstanceRoleARN = strings.TrimSpace(aws.ToString(instance.InstanceRoleArn))
}

func mapNetworkConfiguration(mapped *apprunnerservice.Service, network *awsapprunnertypes.NetworkConfiguration) {
	if network == nil {
		return
	}
	if egress := network.EgressConfiguration; egress != nil {
		mapped.EgressType = strings.TrimSpace(string(egress.EgressType))
		mapped.VpcConnectorARN = strings.TrimSpace(aws.ToString(egress.VpcConnectorArn))
	}
	if ingress := network.IngressConfiguration; ingress != nil {
		mapped.IsPubliclyAccessible = ingress.IsPubliclyAccessible
	}
}

func mapHealthCheck(health *awsapprunnertypes.HealthCheckConfiguration) apprunnerservice.HealthCheck {
	if health == nil {
		return apprunnerservice.HealthCheck{}
	}
	return apprunnerservice.HealthCheck{
		Protocol:           strings.TrimSpace(string(health.Protocol)),
		Path:               strings.TrimSpace(aws.ToString(health.Path)),
		Interval:           aws.ToInt32(health.Interval),
		Timeout:            aws.ToInt32(health.Timeout),
		HealthyThreshold:   aws.ToInt32(health.HealthyThreshold),
		UnhealthyThreshold: aws.ToInt32(health.UnhealthyThreshold),
	}
}

func mapConnection(summary awsapprunnertypes.ConnectionSummary) apprunnerservice.Connection {
	mapped := apprunnerservice.Connection{
		ARN:          strings.TrimSpace(aws.ToString(summary.ConnectionArn)),
		Name:         strings.TrimSpace(aws.ToString(summary.ConnectionName)),
		ProviderType: strings.TrimSpace(string(summary.ProviderType)),
		Status:       strings.TrimSpace(string(summary.Status)),
	}
	if summary.CreatedAt != nil {
		mapped.CreatedAt = *summary.CreatedAt
	}
	return mapped
}

func mapAutoScalingConfiguration(config *awsapprunnertypes.AutoScalingConfiguration) apprunnerservice.AutoScalingConfiguration {
	mapped := apprunnerservice.AutoScalingConfiguration{
		ARN:            strings.TrimSpace(aws.ToString(config.AutoScalingConfigurationArn)),
		Name:           strings.TrimSpace(aws.ToString(config.AutoScalingConfigurationName)),
		Revision:       aws.ToInt32(config.AutoScalingConfigurationRevision),
		Status:         strings.TrimSpace(string(config.Status)),
		IsDefault:      aws.ToBool(config.IsDefault),
		Latest:         aws.ToBool(config.Latest),
		MaxConcurrency: aws.ToInt32(config.MaxConcurrency),
		MaxSize:        aws.ToInt32(config.MaxSize),
		MinSize:        aws.ToInt32(config.MinSize),
	}
	if config.CreatedAt != nil {
		mapped.CreatedAt = *config.CreatedAt
	}
	return mapped
}

func mapObservabilityConfiguration(config *awsapprunnertypes.ObservabilityConfiguration) apprunnerservice.ObservabilityConfiguration {
	mapped := apprunnerservice.ObservabilityConfiguration{
		ARN:      strings.TrimSpace(aws.ToString(config.ObservabilityConfigurationArn)),
		Name:     strings.TrimSpace(aws.ToString(config.ObservabilityConfigurationName)),
		Revision: config.ObservabilityConfigurationRevision,
		Status:   strings.TrimSpace(string(config.Status)),
		Latest:   config.Latest,
	}
	if trace := config.TraceConfiguration; trace != nil {
		mapped.TraceVendor = strings.TrimSpace(string(trace.Vendor))
	}
	if config.CreatedAt != nil {
		mapped.CreatedAt = *config.CreatedAt
	}
	return mapped
}

func mapVpcConnector(connector awsapprunnertypes.VpcConnector) apprunnerservice.VpcConnector {
	mapped := apprunnerservice.VpcConnector{
		ARN:            strings.TrimSpace(aws.ToString(connector.VpcConnectorArn)),
		Name:           strings.TrimSpace(aws.ToString(connector.VpcConnectorName)),
		Revision:       connector.VpcConnectorRevision,
		Status:         strings.TrimSpace(string(connector.Status)),
		Subnets:        cleanStrings(connector.Subnets),
		SecurityGroups: cleanStrings(connector.SecurityGroups),
	}
	if connector.CreatedAt != nil {
		mapped.CreatedAt = *connector.CreatedAt
	}
	return mapped
}

func mapVpcIngressConnection(ingress *awsapprunnertypes.VpcIngressConnection) apprunnerservice.VpcIngressConnection {
	mapped := apprunnerservice.VpcIngressConnection{
		ARN:        strings.TrimSpace(aws.ToString(ingress.VpcIngressConnectionArn)),
		Name:       strings.TrimSpace(aws.ToString(ingress.VpcIngressConnectionName)),
		Status:     strings.TrimSpace(string(ingress.Status)),
		DomainName: strings.TrimSpace(aws.ToString(ingress.DomainName)),
		ServiceARN: strings.TrimSpace(aws.ToString(ingress.ServiceArn)),
	}
	if vpc := ingress.IngressVpcConfiguration; vpc != nil {
		mapped.VpcEndpointID = strings.TrimSpace(aws.ToString(vpc.VpcEndpointId))
		mapped.VpcID = strings.TrimSpace(aws.ToString(vpc.VpcId))
	}
	if ingress.CreatedAt != nil {
		mapped.CreatedAt = *ingress.CreatedAt
	}
	return mapped
}

func mapTags(tags []awsapprunnertypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// secretReferences converts the runtime environment secret map into ARN
// reference records. The map key is the environment-variable name and the value
// is a Secrets Manager / SSM ARN reference, never a resolved secret value.
func secretReferences(secrets map[string]string) []apprunnerservice.SecretReference {
	if len(secrets) == 0 {
		return nil
	}
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	output := make([]apprunnerservice.SecretReference, 0, len(names))
	for _, name := range names {
		output = append(output, apprunnerservice.SecretReference{
			Name:      strings.TrimSpace(name),
			ValueFrom: strings.TrimSpace(secrets[name]),
		})
	}
	return output
}

// sortedKeys returns the deterministically sorted keys of an environment-
// variable map. Only the names are returned; values are never read.
func sortedKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	return keys
}

func cleanStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
