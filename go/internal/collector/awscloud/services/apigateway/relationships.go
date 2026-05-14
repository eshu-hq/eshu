package apigateway

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func domainRelationships(
	boundary awscloud.Boundary,
	domain DomainName,
) []awscloud.RelationshipObservation {
	sourceID := domainResourceID(domain)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	for _, group := range groupedMappings(domain.Mappings) {
		if group.apiID == "" {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayDomainMapsToAPI,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(domain.ARN),
			TargetResourceID: group.apiID,
			TargetARN:        apiARN(boundary.Region, group.apiKind, group.apiID),
			TargetType:       apiResourceType(group.apiKind),
			Attributes: map[string]any{
				"api_kind": group.apiKind,
				"mappings": group.attributes,
			},
			SourceRecordID: sourceID + "->" + group.apiID,
		})
	}
	for _, certificateARN := range cloneStrings(domain.CertificateARNs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayDomainUsesACMCertificate,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(domain.ARN),
			TargetResourceID: certificateARN,
			TargetARN:        certificateARN,
			TargetType:       "aws_acm_certificate",
			Attributes: map[string]any{
				"api_kind": strings.TrimSpace(domain.APIKind),
				"domain":   strings.TrimSpace(domain.Name),
			},
			SourceRecordID: sourceID + "->" + certificateARN,
		})
	}
	return relationships
}

func stageRelationships(
	boundary awscloud.Boundary,
	stage Stage,
	apiARN string,
) []awscloud.RelationshipObservation {
	stageID := stageResourceID(stage.APIID, stage.Name)
	apiID := strings.TrimSpace(stage.APIID)
	if stageID == "" || apiID == "" {
		return nil
	}
	if apiARN == "" {
		apiARN = apiARNForStage(boundary.Region, stage)
	}
	relationships := []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAPIGatewayAPIHasStage,
		SourceResourceID: apiID,
		SourceARN:        apiARN,
		TargetResourceID: stageID,
		TargetARN:        stageARN(boundary.Region, stage.APIKind, stage.APIID, stage.Name),
		TargetType:       awscloud.ResourceTypeAPIGatewayStage,
		Attributes: map[string]any{
			"api_kind":   strings.TrimSpace(stage.APIKind),
			"stage_name": strings.TrimSpace(stage.Name),
		},
		SourceRecordID: apiID + "->" + stageID,
	}}
	if targetARN := strings.TrimSpace(stage.AccessLogDestination); targetARN != "" && isARN(targetARN) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayStageLogsToResource,
			SourceResourceID: stageID,
			SourceARN:        stageARN(boundary.Region, stage.APIKind, stage.APIID, stage.Name),
			TargetResourceID: targetARN,
			TargetARN:        targetARN,
			TargetType:       "aws_resource",
			Attributes: map[string]any{
				"api_kind":   strings.TrimSpace(stage.APIKind),
				"stage_name": strings.TrimSpace(stage.Name),
			},
			SourceRecordID: stageID + "->" + targetARN,
		})
	}
	return relationships
}

func appendIntegrationRelationships(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	apiKind string,
	apiID string,
	sourceARN string,
	integrations []Integration,
) error {
	for _, group := range groupedIntegrations(integrations, apiKind, apiID) {
		groupSourceARN := sourceARN
		if groupSourceARN == "" {
			groupSourceARN = apiARN(boundary.Region, group.apiKind, group.apiID)
		}
		err := appendRelationship(envelopes, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayAPIIntegratesWithResource,
			SourceResourceID: group.apiID,
			SourceARN:        groupSourceARN,
			TargetResourceID: group.targetARN,
			TargetARN:        group.targetARN,
			TargetType:       targetTypeFromARN(group.targetARN),
			Attributes: map[string]any{
				"api_kind":     group.apiKind,
				"integrations": group.attributes,
			},
			SourceRecordID: group.apiID + "->" + group.targetARN,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

type mappingGroup struct {
	apiKind    string
	apiID      string
	attributes []map[string]any
}

func groupedMappings(mappings []Mapping) []mappingGroup {
	groups := make(map[string]mappingGroup)
	for _, mapping := range mappings {
		apiID := strings.TrimSpace(mapping.APIID)
		if apiID == "" {
			continue
		}
		apiKind := strings.TrimSpace(mapping.APIKind)
		key := apiID
		group := groups[key]
		group.apiID = apiID
		attributes := map[string]any{
			"api_kind": apiKind,
			"domain":   strings.TrimSpace(mapping.Domain),
			"key":      strings.TrimSpace(mapping.Key),
			"stage":    strings.TrimSpace(mapping.Stage),
		}
		if id := strings.TrimSpace(mapping.ID); id != "" {
			attributes["id"] = id
		}
		group.attributes = append(group.attributes, attributes)
		groups[key] = group
	}
	output := make([]mappingGroup, 0, len(groups))
	for _, group := range groups {
		sortMappingAttributes(group.attributes)
		if len(group.attributes) > 0 {
			group.apiKind = stringAttribute(group.attributes[0], "api_kind")
		}
		output = append(output, group)
	}
	sort.Slice(output, func(i, j int) bool {
		if output[i].apiID == output[j].apiID {
			return output[i].apiKind < output[j].apiKind
		}
		return output[i].apiID < output[j].apiID
	})
	return output
}

func sortMappingAttributes(attributes []map[string]any) {
	sort.Slice(attributes, func(i, j int) bool {
		left := attributes[i]
		right := attributes[j]
		for _, key := range []string{"key", "stage", "domain", "api_kind", "id"} {
			if stringAttribute(left, key) == stringAttribute(right, key) {
				continue
			}
			return stringAttribute(left, key) < stringAttribute(right, key)
		}
		return false
	})
}

type integrationGroup struct {
	apiKind    string
	apiID      string
	targetARN  string
	attributes []map[string]any
}

func groupedIntegrations(integrations []Integration, fallbackAPIKind, fallbackAPIID string) []integrationGroup {
	groups := make(map[string]integrationGroup)
	for _, integration := range integrations {
		apiID := firstNonEmpty(integration.APIID, fallbackAPIID)
		apiKind := firstNonEmpty(integration.APIKind, fallbackAPIKind)
		targetARN := targetARNFromIntegrationURI(integration.URI)
		if apiID == "" || targetARN == "" {
			continue
		}
		key := apiID + "\x00" + targetARN
		group := groups[key]
		group.apiID = apiID
		group.targetARN = targetARN
		group.attributes = append(group.attributes, integrationAttributes(integration, apiKind))
		groups[key] = group
	}
	output := make([]integrationGroup, 0, len(groups))
	for _, group := range groups {
		sortIntegrationAttributes(group.attributes)
		if len(group.attributes) > 0 {
			group.apiKind = stringAttribute(group.attributes[0], "api_kind")
		}
		output = append(output, group)
	}
	sort.Slice(output, func(i, j int) bool {
		if output[i].apiID != output[j].apiID {
			return output[i].apiID < output[j].apiID
		}
		if output[i].targetARN != output[j].targetARN {
			return output[i].targetARN < output[j].targetARN
		}
		return output[i].apiKind < output[j].apiKind
	})
	return output
}

func integrationAttributes(integration Integration, apiKind string) map[string]any {
	attributes := map[string]any{
		"api_gateway_managed": boolOrNil(integration.APIGatewayManaged),
		"api_kind":            apiKind,
		"connection_id":       strings.TrimSpace(integration.ConnectionID),
		"connection_type":     strings.TrimSpace(integration.ConnectionType),
		"integration_id":      strings.TrimSpace(integration.IntegrationID),
		"integration_type":    strings.TrimSpace(integration.Type),
		"method":              strings.TrimSpace(integration.Method),
		"resource_id":         strings.TrimSpace(integration.ResourceID),
		"resource_path":       strings.TrimSpace(integration.ResourcePath),
		"timeout_millis":      integration.TimeoutMillis,
	}
	if version := strings.TrimSpace(integration.PayloadFormatVersion); version != "" {
		attributes["payload_format_version"] = version
	}
	return attributes
}

func sortIntegrationAttributes(attributes []map[string]any) {
	sort.Slice(attributes, func(i, j int) bool {
		left := attributes[i]
		right := attributes[j]
		for _, key := range []string{"resource_path", "method", "integration_id", "resource_id", "api_kind"} {
			if stringAttribute(left, key) == stringAttribute(right, key) {
				continue
			}
			return stringAttribute(left, key) < stringAttribute(right, key)
		}
		return false
	})
}

func stringAttribute(attributes map[string]any, key string) string {
	value, _ := attributes[key].(string)
	return value
}

func restAPIARN(region, apiID string) string {
	if strings.TrimSpace(apiID) == "" {
		return ""
	}
	return "arn:aws:apigateway:" + strings.TrimSpace(region) + "::/restapis/" + strings.TrimSpace(apiID)
}

func v2APIARN(region, apiID string) string {
	if strings.TrimSpace(apiID) == "" {
		return ""
	}
	return "arn:aws:apigateway:" + strings.TrimSpace(region) + "::/apis/" + strings.TrimSpace(apiID)
}

func stageARN(region, apiKind, apiID, stageName string) string {
	apiID = strings.TrimSpace(apiID)
	stageName = strings.TrimSpace(stageName)
	if apiID == "" || stageName == "" {
		return ""
	}
	if apiKind == APIKindV2 {
		return v2APIARN(region, apiID) + "/stages/" + stageName
	}
	return restAPIARN(region, apiID) + "/stages/" + stageName
}

func apiARN(region, apiKind, apiID string) string {
	if apiKind == APIKindV2 {
		return v2APIARN(region, apiID)
	}
	return restAPIARN(region, apiID)
}

func apiARNForStage(region string, stage Stage) string {
	return apiARN(region, stage.APIKind, stage.APIID)
}

func targetARNFromIntegrationURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "arn:aws:apigateway:") {
		if idx := strings.Index(uri, "/functions/"); idx >= 0 {
			candidate := uri[idx+len("/functions/"):]
			if end := strings.Index(candidate, "/invocations"); end >= 0 {
				return strings.TrimSpace(candidate[:end])
			}
		}
		return ""
	}
	if isARN(uri) {
		return uri
	}
	return ""
}

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

func targetTypeFromARN(arn string) string {
	parts := strings.Split(strings.TrimSpace(arn), ":")
	if len(parts) < 6 {
		return "aws_resource"
	}
	switch parts[2] {
	case "lambda":
		return "aws_lambda_function"
	case "elasticloadbalancing":
		return "aws_elbv2_listener"
	case "servicediscovery":
		return "aws_cloudmap_service"
	case "logs":
		return "aws_cloudwatch_logs_log_group"
	default:
		return "aws_resource"
	}
}
