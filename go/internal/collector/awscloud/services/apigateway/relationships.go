package apigateway

import (
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
	for _, mapping := range domain.Mappings {
		if strings.TrimSpace(mapping.APIID) == "" {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayDomainMapsToAPI,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(domain.ARN),
			TargetResourceID: strings.TrimSpace(mapping.APIID),
			TargetARN:        apiARN(boundary.Region, mapping.APIKind, mapping.APIID),
			TargetType:       apiResourceType(mapping.APIKind),
			Attributes: map[string]any{
				"api_kind": strings.TrimSpace(mapping.APIKind),
				"domain":   strings.TrimSpace(mapping.Domain),
				"key":      strings.TrimSpace(mapping.Key),
				"stage":    strings.TrimSpace(mapping.Stage),
			},
			SourceRecordID: sourceID + "->" + strings.TrimSpace(mapping.APIID),
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
	for _, integration := range integrations {
		if strings.TrimSpace(integration.APIID) == "" {
			integration.APIID = apiID
		}
		if strings.TrimSpace(integration.APIKind) == "" {
			integration.APIKind = apiKind
		}
		targetARN := targetARNFromIntegrationURI(integration.URI)
		if targetARN == "" {
			continue
		}
		if sourceARN == "" {
			sourceARN = apiARN(boundary.Region, integration.APIKind, integration.APIID)
		}
		err := appendRelationship(envelopes, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayAPIIntegratesWithResource,
			SourceResourceID: strings.TrimSpace(integration.APIID),
			SourceARN:        sourceARN,
			TargetResourceID: targetARN,
			TargetARN:        targetARN,
			TargetType:       targetTypeFromARN(targetARN),
			Attributes: map[string]any{
				"api_kind":               strings.TrimSpace(integration.APIKind),
				"integration_id":         strings.TrimSpace(integration.IntegrationID),
				"resource_id":            strings.TrimSpace(integration.ResourceID),
				"resource_path":          strings.TrimSpace(integration.ResourcePath),
				"method":                 strings.TrimSpace(integration.Method),
				"integration_type":       strings.TrimSpace(integration.Type),
				"connection_type":        strings.TrimSpace(integration.ConnectionType),
				"connection_id":          strings.TrimSpace(integration.ConnectionID),
				"payload_format_version": strings.TrimSpace(integration.PayloadFormatVersion),
				"timeout_millis":         integration.TimeoutMillis,
				"api_gateway_managed":    boolOrNil(integration.APIGatewayManaged),
			},
			SourceRecordID: strings.TrimSpace(integration.APIID) + "->" + targetARN,
		})
		if err != nil {
			return err
		}
	}
	return nil
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
