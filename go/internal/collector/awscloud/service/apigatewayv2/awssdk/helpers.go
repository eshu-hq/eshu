// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

func accessLogDestination(input *awsapigatewayv2types.AccessLogSettings) string {
	if input == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(input.DestinationArn))
}

func domainEndpointTypes(configs []awsapigatewayv2types.DomainNameConfiguration) []string {
	values := make([]string, 0, len(configs))
	for _, config := range configs {
		if value := strings.TrimSpace(string(config.EndpointType)); value != "" {
			values = append(values, value)
		}
	}
	return cloneStrings(values)
}

func domainCertificateARNs(configs []awsapigatewayv2types.DomainNameConfiguration) []string {
	values := make([]string, 0, len(configs)*2)
	for _, config := range configs {
		values = append(
			values,
			aws.ToString(config.CertificateArn),
			aws.ToString(config.OwnershipVerificationCertificateArn),
		)
	}
	return cloneStrings(values)
}

func firstDomainStatus(configs []awsapigatewayv2types.DomainNameConfiguration) string {
	for _, config := range configs {
		if value := strings.TrimSpace(string(config.DomainNameStatus)); value != "" {
			return value
		}
	}
	return ""
}

func firstAPIGatewayDomainName(configs []awsapigatewayv2types.DomainNameConfiguration) string {
	for _, config := range configs {
		if value := strings.TrimSpace(aws.ToString(config.ApiGatewayDomainName)); value != "" {
			return value
		}
	}
	return ""
}

func firstHostedZoneID(configs []awsapigatewayv2types.DomainNameConfiguration) string {
	for _, config := range configs {
		if value := strings.TrimSpace(aws.ToString(config.HostedZoneId)); value != "" {
			return value
		}
	}
	return ""
}

func firstSecurityPolicy(configs []awsapigatewayv2types.DomainNameConfiguration) string {
	for _, config := range configs {
		if value := strings.TrimSpace(string(config.SecurityPolicy)); value != "" {
			return value
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			output[trimmed] = value
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	output := make([]string, 0, len(input))
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneBool(input *bool) *bool {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}
