package awssdk

import (
	"strings"

	awsapigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

func mapEndpointConfiguration(input *awsapigatewaytypes.EndpointConfiguration) ([]string, []string) {
	if input == nil {
		return nil, nil
	}
	types := make([]string, 0, len(input.Types))
	for _, value := range input.Types {
		if trimmed := strings.TrimSpace(string(value)); trimmed != "" {
			types = append(types, trimmed)
		}
	}
	return cloneStrings(types), cloneStrings(input.VpcEndpointIds)
}

func accessLogDestinationREST(input *awsapigatewaytypes.AccessLogSettings) string {
	if input == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(input.DestinationArn))
}

func accessLogDestinationV2(input *awsapigatewayv2types.AccessLogSettings) string {
	if input == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(input.DestinationArn))
}

func mapV2DomainEndpointTypes(configs []awsapigatewayv2types.DomainNameConfiguration) []string {
	values := make([]string, 0, len(configs))
	for _, config := range configs {
		if value := strings.TrimSpace(string(config.EndpointType)); value != "" {
			values = append(values, value)
		}
	}
	return cloneStrings(values)
}

func mapV2DomainCertificateARNs(configs []awsapigatewayv2types.DomainNameConfiguration) []string {
	values := make([]string, 0, len(configs)*2)
	for _, config := range configs {
		values = append(values, stringValue(config.CertificateArn), stringValue(config.OwnershipVerificationCertificateArn))
	}
	return cloneStrings(values)
}

func firstV2DomainStatus(configs []awsapigatewayv2types.DomainNameConfiguration) string {
	for _, config := range configs {
		if value := strings.TrimSpace(string(config.DomainNameStatus)); value != "" {
			return value
		}
	}
	return ""
}

func firstV2APIGatewayDomainName(configs []awsapigatewayv2types.DomainNameConfiguration) string {
	for _, config := range configs {
		if value := strings.TrimSpace(stringValue(config.ApiGatewayDomainName)); value != "" {
			return value
		}
	}
	return ""
}

func firstV2HostedZoneID(configs []awsapigatewayv2types.DomainNameConfiguration) string {
	for _, config := range configs {
		if value := strings.TrimSpace(stringValue(config.HostedZoneId)); value != "" {
			return value
		}
	}
	return ""
}

func firstV2SecurityPolicy(configs []awsapigatewayv2types.DomainNameConfiguration) string {
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

func stringValue(input *string) string {
	if input == nil {
		return ""
	}
	return *input
}
