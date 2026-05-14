package apigateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS API Gateway metadata facts for one claimed account and
// region. It never reads request or response payloads, API keys, policy JSON,
// integration credentials, authorizer secrets, or mutates API Gateway resources.
type Scanner struct {
	Client Client
}

// Scan observes REST, HTTP, WebSocket, custom-domain, stage, mapping, and
// integration metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("apigateway scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceAPIGateway
	case awscloud.ServiceAPIGateway:
	default:
		return nil, fmt.Errorf("apigateway scanner received service_kind %q", boundary.ServiceKind)
	}
	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot API Gateway metadata: %w", err)
	}
	var envelopes []facts.Envelope
	for _, api := range snapshot.RESTAPIs {
		if err := appendResource(&envelopes, restAPIObservation(boundary, api)); err != nil {
			return nil, err
		}
		if err := appendStageFacts(&envelopes, boundary, APIKindREST, restAPIResourceID(api), "", api.Stages); err != nil {
			return nil, err
		}
		if err := appendIntegrationRelationships(&envelopes, boundary, APIKindREST, restAPIResourceID(api), "", api.Integrations); err != nil {
			return nil, err
		}
	}
	for _, api := range snapshot.V2APIs {
		if err := appendResource(&envelopes, v2APIObservation(boundary, api)); err != nil {
			return nil, err
		}
		if err := appendStageFacts(&envelopes, boundary, APIKindV2, v2APIResourceID(api), "", api.Stages); err != nil {
			return nil, err
		}
		if err := appendIntegrationRelationships(&envelopes, boundary, APIKindV2, v2APIResourceID(api), "", api.Integrations); err != nil {
			return nil, err
		}
	}
	for _, domain := range snapshot.Domains {
		if err := appendResource(&envelopes, domainObservation(boundary, domain)); err != nil {
			return nil, err
		}
		for _, relationship := range domainRelationships(boundary, domain) {
			if err := appendRelationship(&envelopes, relationship); err != nil {
				return nil, err
			}
		}
	}
	return envelopes, nil
}

func appendResource(envelopes *[]facts.Envelope, observation awscloud.ResourceObservation) error {
	envelope, err := awscloud.NewResourceEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func appendRelationship(envelopes *[]facts.Envelope, observation awscloud.RelationshipObservation) error {
	envelope, err := awscloud.NewRelationshipEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func restAPIObservation(boundary awscloud.Boundary, api RESTAPI) awscloud.ResourceObservation {
	apiID := restAPIResourceID(api)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          restAPIARN(boundary.Region, apiID),
		ResourceID:   apiID,
		ResourceType: awscloud.ResourceTypeAPIGatewayRESTAPI,
		Name:         firstNonEmpty(api.Name, apiID),
		State:        strings.TrimSpace(api.APIStatus),
		Tags:         cloneStringMap(api.Tags),
		Attributes: map[string]any{
			"api_id":                       apiID,
			"api_kind":                     APIKindREST,
			"description":                  strings.TrimSpace(api.Description),
			"created_date":                 timeOrNil(api.CreatedDate),
			"version":                      strings.TrimSpace(api.Version),
			"api_status":                   strings.TrimSpace(api.APIStatus),
			"api_key_source":               strings.TrimSpace(api.APIKeySource),
			"disable_execute_api_endpoint": api.DisableExecuteAPIEndpoint,
			"endpoint_types":               cloneStrings(api.EndpointTypes),
			"vpc_endpoint_ids":             cloneStrings(api.VPCEndpointIDs),
		},
		CorrelationAnchors: []string{apiID, api.Name},
		SourceRecordID:     apiID,
	}
}

func v2APIObservation(boundary awscloud.Boundary, api V2API) awscloud.ResourceObservation {
	apiID := v2APIResourceID(api)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          v2APIARN(boundary.Region, apiID),
		ResourceID:   apiID,
		ResourceType: awscloud.ResourceTypeAPIGatewayV2API,
		Name:         firstNonEmpty(api.Name, apiID),
		State:        strings.TrimSpace(api.ProtocolType),
		Tags:         cloneStringMap(api.Tags),
		Attributes: map[string]any{
			"api_id":                       apiID,
			"api_kind":                     APIKindV2,
			"protocol_type":                strings.TrimSpace(api.ProtocolType),
			"endpoint":                     strings.TrimSpace(api.Endpoint),
			"created_date":                 timeOrNil(api.CreatedDate),
			"description":                  strings.TrimSpace(api.Description),
			"disable_execute_api_endpoint": api.DisableExecuteAPIEndpoint,
			"api_gateway_managed":          boolOrNil(api.APIGatewayManaged),
			"ip_address_type":              strings.TrimSpace(api.IPAddressType),
		},
		CorrelationAnchors: []string{apiID, api.Name, api.Endpoint},
		SourceRecordID:     apiID,
	}
}

func domainObservation(boundary awscloud.Boundary, domain DomainName) awscloud.ResourceObservation {
	resourceID := domainResourceID(domain)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(domain.ARN),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAPIGatewayDomainName,
		Name:         firstNonEmpty(domain.Name, resourceID),
		State:        strings.TrimSpace(domain.Status),
		Tags:         cloneStringMap(domain.Tags),
		Attributes: map[string]any{
			"api_kind":                    strings.TrimSpace(domain.APIKind),
			"domain_name":                 strings.TrimSpace(domain.Name),
			"status":                      strings.TrimSpace(domain.Status),
			"endpoint_types":              cloneStrings(domain.EndpointTypes),
			"regional_domain_name":        strings.TrimSpace(domain.RegionalDomain),
			"regional_hosted_zone_id":     strings.TrimSpace(domain.RegionalZoneID),
			"distribution_domain_name":    strings.TrimSpace(domain.DistributionName),
			"distribution_hosted_zone_id": strings.TrimSpace(domain.DistributionZone),
			"certificate_arns":            cloneStrings(domain.CertificateARNs),
			"security_policy":             strings.TrimSpace(domain.SecurityPolicy),
			"api_mapping_selection":       strings.TrimSpace(domain.APIMappingSelect),
			"mappings":                    mappingAttributes(domain.Mappings),
		},
		CorrelationAnchors: []string{domain.Name, domain.ARN},
		SourceRecordID:     resourceID,
	}
}

func appendStageFacts(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	apiKind string,
	apiID string,
	apiARN string,
	stages []Stage,
) error {
	for _, stage := range stages {
		if strings.TrimSpace(stage.APIID) == "" {
			stage.APIID = apiID
		}
		if strings.TrimSpace(stage.APIKind) == "" {
			stage.APIKind = apiKind
		}
		if strings.TrimSpace(stage.Name) == "" {
			continue
		}
		if err := appendResource(envelopes, stageObservation(boundary, stage)); err != nil {
			return err
		}
		for _, relationship := range stageRelationships(boundary, stage, apiARN) {
			if err := appendRelationship(envelopes, relationship); err != nil {
				return err
			}
		}
	}
	return nil
}

func stageObservation(boundary awscloud.Boundary, stage Stage) awscloud.ResourceObservation {
	resourceID := stageResourceID(stage.APIID, stage.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          stageARN(boundary.Region, stage.APIKind, stage.APIID, stage.Name),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAPIGatewayStage,
		Name:         strings.TrimSpace(stage.Name),
		Tags:         cloneStringMap(stage.Tags),
		Attributes: map[string]any{
			"api_kind":                   strings.TrimSpace(stage.APIKind),
			"api_id":                     strings.TrimSpace(stage.APIID),
			"stage_name":                 strings.TrimSpace(stage.Name),
			"deployment_id":              strings.TrimSpace(stage.DeploymentID),
			"description":                strings.TrimSpace(stage.Description),
			"created_date":               timeOrNil(stage.CreatedDate),
			"last_updated_date":          timeOrNil(stage.LastUpdatedDate),
			"cache_cluster_enabled":      stage.CacheClusterEnabled,
			"cache_cluster_size":         strings.TrimSpace(stage.CacheClusterSize),
			"cache_cluster_status":       strings.TrimSpace(stage.CacheClusterStatus),
			"tracing_enabled":            stage.TracingEnabled,
			"client_certificate_id":      strings.TrimSpace(stage.ClientCertificateID),
			"access_log_destination_arn": strings.TrimSpace(stage.AccessLogDestination),
			"web_acl_arn":                strings.TrimSpace(stage.WebACLARN),
			"auto_deploy":                boolOrNil(stage.AutoDeploy),
			"api_gateway_managed":        boolOrNil(stage.APIGatewayManaged),
		},
		CorrelationAnchors: []string{resourceID, stage.APIID, stage.Name},
		SourceRecordID:     resourceID,
	}
}

func mappingAttributes(mappings []Mapping) []map[string]any {
	if len(mappings) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(mappings))
	for _, mapping := range mappings {
		if strings.TrimSpace(mapping.APIID) == "" {
			continue
		}
		attributes := map[string]any{
			"api_kind": strings.TrimSpace(mapping.APIKind),
			"api_id":   strings.TrimSpace(mapping.APIID),
			"key":      strings.TrimSpace(mapping.Key),
			"stage":    strings.TrimSpace(mapping.Stage),
		}
		if id := strings.TrimSpace(mapping.ID); id != "" {
			attributes["id"] = id
		}
		output = append(output, attributes)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
