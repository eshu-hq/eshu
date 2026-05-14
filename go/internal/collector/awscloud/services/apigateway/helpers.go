package apigateway

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

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

func boolOrNil(input *bool) any {
	if input == nil {
		return nil
	}
	return *input
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func restAPIResourceID(api RESTAPI) string {
	return strings.TrimSpace(api.ID)
}

func v2APIResourceID(api V2API) string {
	return strings.TrimSpace(api.ID)
}

func stageResourceID(apiID, stageName string) string {
	return strings.TrimSpace(apiID) + "/stages/" + strings.TrimSpace(stageName)
}

func domainResourceID(domain DomainName) string {
	return firstNonEmpty(domain.Name, domain.ARN)
}

func apiResourceType(apiKind string) string {
	if apiKind == APIKindV2 {
		return awscloud.ResourceTypeAPIGatewayV2API
	}
	return awscloud.ResourceTypeAPIGatewayRESTAPI
}
