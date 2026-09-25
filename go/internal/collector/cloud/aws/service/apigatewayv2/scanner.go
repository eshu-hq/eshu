// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apigatewayv2

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS API Gateway v2 metadata facts for one claimed account and
// region. It covers HTTP and WebSocket APIs, their stages, routes,
// integrations, authorizers, custom domains, and VPC links. It never reads
// request/response mapping templates, route request models, authorizer
// invocation URIs or credentials, JWT secrets, or stage variable values, and it
// never mutates API Gateway resources. The classic REST (v1) surface is owned
// by the separate apigateway scanner.
type Scanner struct {
	Client Client
}

// Scan observes API Gateway v2 API, stage, route, integration, authorizer,
// custom-domain, and VPC link metadata through the configured client and emits
// resource and relationship facts.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("apigatewayv2 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceAPIGatewayV2:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceAPIGatewayV2
	default:
		return nil, fmt.Errorf("apigatewayv2 scanner received service_kind %q", boundary.ServiceKind)
	}
	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot API Gateway v2 metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, api := range snapshot.APIs {
		if err := appendAPI(&envelopes, boundary, api); err != nil {
			return nil, err
		}
	}
	for _, link := range snapshot.VPCLinks {
		if err := appendVPCLink(&envelopes, boundary, link); err != nil {
			return nil, err
		}
	}
	for _, domain := range snapshot.Domains {
		if err := appendDomain(&envelopes, boundary, domain); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func appendAPI(envelopes *[]facts.Envelope, boundary aws.Boundary, api API) error {
	apiID := strings.TrimSpace(api.ID)
	if apiID == "" {
		return nil
	}
	if err := appendResource(envelopes, apiObservation(boundary, api)); err != nil {
		return err
	}
	for _, relationship := range apiAuthRelationships(boundary, api) {
		if err := appendRelationship(envelopes, relationship); err != nil {
			return err
		}
	}
	for _, stage := range api.Stages {
		if strings.TrimSpace(stage.Name) == "" {
			continue
		}
		stage.APIID = firstNonEmpty(stage.APIID, apiID)
		if err := appendResource(envelopes, stageObservation(boundary, stage)); err != nil {
			return err
		}
		if relationship, ok := apiHasStageRelationship(boundary, api, stage); ok {
			if err := appendRelationship(envelopes, relationship); err != nil {
				return err
			}
		}
	}
	for _, authorizer := range api.Authorizers {
		if strings.TrimSpace(authorizer.AuthorizerID) == "" {
			continue
		}
		authorizer.APIID = firstNonEmpty(authorizer.APIID, apiID)
		if err := appendResource(envelopes, authorizerObservation(boundary, authorizer)); err != nil {
			return err
		}
	}
	for _, integration := range api.Integrations {
		if strings.TrimSpace(integration.IntegrationID) == "" {
			continue
		}
		integration.APIID = firstNonEmpty(integration.APIID, apiID)
		if err := appendResource(envelopes, integrationObservation(boundary, integration)); err != nil {
			return err
		}
		for _, relationship := range integrationRelationships(boundary, api, integration) {
			if err := appendRelationship(envelopes, relationship); err != nil {
				return err
			}
		}
	}
	for _, route := range api.Routes {
		if strings.TrimSpace(route.RouteID) == "" {
			continue
		}
		route.APIID = firstNonEmpty(route.APIID, apiID)
		if err := appendResource(envelopes, routeObservation(boundary, route)); err != nil {
			return err
		}
		for _, relationship := range routeRelationships(boundary, api, route) {
			if err := appendRelationship(envelopes, relationship); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendVPCLink(envelopes *[]facts.Envelope, boundary aws.Boundary, link VPCLink) error {
	if strings.TrimSpace(link.ID) == "" {
		return nil
	}
	if err := appendResource(envelopes, vpcLinkObservation(boundary, link)); err != nil {
		return err
	}
	for _, relationship := range vpcLinkRelationships(boundary, link) {
		if err := appendRelationship(envelopes, relationship); err != nil {
			return err
		}
	}
	return nil
}

func appendDomain(envelopes *[]facts.Envelope, boundary aws.Boundary, domain DomainName) error {
	if domainResourceID(domain) == "" {
		return nil
	}
	if err := appendResource(envelopes, domainObservation(boundary, domain)); err != nil {
		return err
	}
	for _, relationship := range domainRelationships(boundary, domain) {
		if err := appendRelationship(envelopes, relationship); err != nil {
			return err
		}
	}
	return nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []aws.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := aws.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func appendResource(envelopes *[]facts.Envelope, observation aws.ResourceObservation) error {
	envelope, err := aws.NewResourceEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func appendRelationship(envelopes *[]facts.Envelope, observation aws.RelationshipObservation) error {
	envelope, err := aws.NewRelationshipEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func apiObservation(boundary aws.Boundary, api API) aws.ResourceObservation {
	apiID := strings.TrimSpace(api.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          apiARN(boundary.Region, apiID),
		ResourceID:   apiID,
		ResourceType: aws.ResourceTypeAPIGatewayV2API,
		Name:         firstNonEmpty(api.Name, apiID),
		State:        strings.TrimSpace(api.ProtocolType),
		Tags:         cloneStringMap(api.Tags),
		Attributes: map[string]any{
			"api_id":                       apiID,
			"protocol_type":                strings.TrimSpace(api.ProtocolType),
			"endpoint":                     strings.TrimSpace(api.Endpoint),
			"created_date":                 timeOrNil(api.CreatedDate),
			"description":                  strings.TrimSpace(api.Description),
			"version":                      strings.TrimSpace(api.Version),
			"disable_execute_api_endpoint": api.DisableExecuteAPIEndpoint,
			"api_gateway_managed":          boolOrNil(api.APIGatewayManaged),
			"ip_address_type":              strings.TrimSpace(api.IPAddressType),
		},
		CorrelationAnchors: []string{apiID, api.Name, api.Endpoint},
		SourceRecordID:     apiID,
	}
}

func stageObservation(boundary aws.Boundary, stage Stage) aws.ResourceObservation {
	resourceID := stageResourceID(stage.APIID, stage.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          stageARN(boundary.Region, stage.APIID, stage.Name),
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAPIGatewayStage,
		Name:         strings.TrimSpace(stage.Name),
		Tags:         cloneStringMap(stage.Tags),
		Attributes: map[string]any{
			"api_kind":                   "v2",
			"api_id":                     strings.TrimSpace(stage.APIID),
			"stage_name":                 strings.TrimSpace(stage.Name),
			"deployment_id":              strings.TrimSpace(stage.DeploymentID),
			"description":                strings.TrimSpace(stage.Description),
			"created_date":               timeOrNil(stage.CreatedDate),
			"last_updated_date":          timeOrNil(stage.LastUpdatedDate),
			"auto_deploy":                boolOrNil(stage.AutoDeploy),
			"api_gateway_managed":        boolOrNil(stage.APIGatewayManaged),
			"client_certificate_id":      strings.TrimSpace(stage.ClientCertificateID),
			"access_log_destination_arn": strings.TrimSpace(stage.AccessLogDestination),
		},
		CorrelationAnchors: []string{resourceID, stage.APIID, stage.Name},
		SourceRecordID:     resourceID,
	}
}

func routeObservation(boundary aws.Boundary, route Route) aws.ResourceObservation {
	resourceID := routeResourceID(route.APIID, route.RouteID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAPIGatewayV2Route,
		Name:         firstNonEmpty(route.RouteKey, route.RouteID),
		Attributes: map[string]any{
			"api_id":              strings.TrimSpace(route.APIID),
			"route_id":            strings.TrimSpace(route.RouteID),
			"route_key":           strings.TrimSpace(route.RouteKey),
			"target":              strings.TrimSpace(route.Target),
			"authorization_type":  strings.TrimSpace(route.AuthorizationType),
			"authorizer_id":       strings.TrimSpace(route.AuthorizerID),
			"api_key_required":    boolOrNil(route.APIKeyRequired),
			"api_gateway_managed": boolOrNil(route.APIGatewayManaged),
			"operation_name":      strings.TrimSpace(route.OperationName),
		},
		CorrelationAnchors: []string{resourceID, route.RouteID, route.RouteKey},
		SourceRecordID:     resourceID,
	}
}

func integrationObservation(boundary aws.Boundary, integration Integration) aws.ResourceObservation {
	resourceID := integrationResourceID(integration.APIID, integration.IntegrationID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAPIGatewayV2Integration,
		Name:         firstNonEmpty(integration.IntegrationID, resourceID),
		State:        strings.TrimSpace(integration.Type),
		Attributes: map[string]any{
			"api_id":                 strings.TrimSpace(integration.APIID),
			"integration_id":         strings.TrimSpace(integration.IntegrationID),
			"integration_type":       strings.TrimSpace(integration.Type),
			"integration_subtype":    strings.TrimSpace(integration.Subtype),
			"integration_uri":        strings.TrimSpace(integration.URI),
			"integration_method":     strings.TrimSpace(integration.Method),
			"connection_type":        strings.TrimSpace(integration.ConnectionType),
			"connection_id":          strings.TrimSpace(integration.ConnectionID),
			"payload_format_version": strings.TrimSpace(integration.PayloadFormatVersion),
			"description":            strings.TrimSpace(integration.Description),
			"timeout_millis":         integration.TimeoutMillis,
			"api_gateway_managed":    boolOrNil(integration.APIGatewayManaged),
		},
		CorrelationAnchors: []string{resourceID, integration.IntegrationID},
		SourceRecordID:     resourceID,
	}
}

func authorizerObservation(boundary aws.Boundary, authorizer Authorizer) aws.ResourceObservation {
	resourceID := authorizerResourceID(authorizer.APIID, authorizer.AuthorizerID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAPIGatewayV2Authorizer,
		Name:         firstNonEmpty(authorizer.Name, authorizer.AuthorizerID),
		State:        strings.TrimSpace(authorizer.Type),
		Attributes: map[string]any{
			"api_id":           strings.TrimSpace(authorizer.APIID),
			"authorizer_id":    strings.TrimSpace(authorizer.AuthorizerID),
			"authorizer_type":  strings.TrimSpace(authorizer.Type),
			"identity_sources": cloneStrings(authorizer.IdentitySource),
			"jwt_issuer":       strings.TrimSpace(authorizer.JWTIssuer),
			"jwt_audience":     cloneStrings(authorizer.JWTAudience),
		},
		CorrelationAnchors: []string{resourceID, authorizer.AuthorizerID, authorizer.Name},
		SourceRecordID:     resourceID,
	}
}

func vpcLinkObservation(boundary aws.Boundary, link VPCLink) aws.ResourceObservation {
	linkID := strings.TrimSpace(link.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   linkID,
		ResourceType: aws.ResourceTypeAPIGatewayV2VPCLink,
		Name:         firstNonEmpty(link.Name, linkID),
		State:        strings.TrimSpace(link.Status),
		Tags:         cloneStringMap(link.Tags),
		Attributes: map[string]any{
			"vpc_link_id":        linkID,
			"status":             strings.TrimSpace(link.Status),
			"status_message":     strings.TrimSpace(link.StatusMessage),
			"version":            strings.TrimSpace(link.Version),
			"subnet_ids":         cloneStrings(link.SubnetIDs),
			"security_group_ids": cloneStrings(link.SecurityGroupIDs),
			"created_date":       timeOrNil(link.CreatedDate),
		},
		CorrelationAnchors: []string{linkID, link.Name},
		SourceRecordID:     linkID,
	}
}

func domainObservation(boundary aws.Boundary, domain DomainName) aws.ResourceObservation {
	resourceID := domainResourceID(domain)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(domain.ARN),
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAPIGatewayDomainName,
		Name:         firstNonEmpty(domain.Name, resourceID),
		State:        strings.TrimSpace(domain.Status),
		Tags:         cloneStringMap(domain.Tags),
		Attributes: map[string]any{
			"api_kind":                "v2",
			"domain_name":             strings.TrimSpace(domain.Name),
			"status":                  strings.TrimSpace(domain.Status),
			"endpoint_types":          cloneStrings(domain.EndpointTypes),
			"regional_domain_name":    strings.TrimSpace(domain.RegionalDomain),
			"regional_hosted_zone_id": strings.TrimSpace(domain.RegionalZoneID),
			"certificate_arns":        cloneStrings(domain.CertificateARNs),
			"security_policy":         strings.TrimSpace(domain.SecurityPolicy),
			"api_mapping_selection":   strings.TrimSpace(domain.APIMappingSelect),
			"mappings":                mappingAttributes(domain.Mappings),
		},
		CorrelationAnchors: []string{domain.Name, domain.ARN},
		SourceRecordID:     resourceID,
	}
}
