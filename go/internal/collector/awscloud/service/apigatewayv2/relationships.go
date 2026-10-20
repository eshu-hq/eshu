// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apigatewayv2

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// apiHasStageRelationship records stage membership on a v2 API.
func apiHasStageRelationship(boundary awscloud.Boundary, api API, stage Stage) (awscloud.RelationshipObservation, bool) {
	apiID := strings.TrimSpace(api.ID)
	stageID := stageResourceID(stage.APIID, stage.Name)
	if apiID == "" || strings.TrimSpace(stage.Name) == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAPIGatewayV2APIHasStage,
		SourceResourceID: apiID,
		SourceARN:        apiARN(boundary.Region, apiID),
		TargetResourceID: stageID,
		TargetARN:        stageARN(boundary.Region, stage.APIID, stage.Name),
		TargetType:       awscloud.ResourceTypeAPIGatewayStage,
		Attributes: map[string]any{
			"stage_name": strings.TrimSpace(stage.Name),
		},
		SourceRecordID: apiID + "#stage#" + stageID,
	}, true
}

// apiAuthRelationships records the Cognito user pools and JWT issuers the API's
// JWT authorizers trust. The Cognito user pool edge joins by the bare pool id
// parsed from the issuer URL, matching the resource_id the Cognito scanner
// publishes for the user pool node; targeting the full issuer URL or the
// "cognito-idp.<region>.amazonaws.com/<poolId>" string would dangle, the same
// defect fixed in the Cognito and AppSync scanners.
func apiAuthRelationships(boundary awscloud.Boundary, api API) []awscloud.RelationshipObservation {
	apiID := strings.TrimSpace(api.ID)
	if apiID == "" {
		return nil
	}
	apiARNValue := apiARN(boundary.Region, apiID)
	seenPools := make(map[string]struct{})
	seenIssuers := make(map[string]struct{})
	var relationships []awscloud.RelationshipObservation
	for _, authorizer := range api.Authorizers {
		issuer := strings.TrimSpace(authorizer.JWTIssuer)
		if issuer == "" {
			continue
		}
		if poolID := userPoolIDFromIssuer(issuer); poolID != "" {
			if _, ok := seenPools[poolID]; ok {
				continue
			}
			seenPools[poolID] = struct{}{}
			relationships = append(relationships, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipAPIGatewayV2APIUsesUserPool,
				SourceResourceID: apiID,
				SourceARN:        apiARNValue,
				TargetResourceID: poolID,
				TargetType:       awscloud.ResourceTypeCognitoUserPool,
				Attributes: map[string]any{
					"authorizer_id": strings.TrimSpace(authorizer.AuthorizerID),
					"issuer":        issuer,
				},
				SourceRecordID: apiID + "#user-pool#" + poolID,
			})
			continue
		}
		if _, ok := seenIssuers[issuer]; ok {
			continue
		}
		seenIssuers[issuer] = struct{}{}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2APIUsesJWTIssuer,
			SourceResourceID: apiID,
			SourceARN:        apiARNValue,
			TargetResourceID: issuer,
			TargetType:       awscloud.APIGatewayV2JWTIssuerTargetType,
			Attributes: map[string]any{
				"authorizer_id": strings.TrimSpace(authorizer.AuthorizerID),
			},
			SourceRecordID: apiID + "#jwt-issuer#" + issuer,
		})
	}
	return relationships
}

// routeRelationships records the API-to-route edge and the route-to-integration
// edge derived from the route target reference.
func routeRelationships(boundary awscloud.Boundary, api API, route Route) []awscloud.RelationshipObservation {
	apiID := strings.TrimSpace(api.ID)
	routeID := routeResourceID(route.APIID, route.RouteID)
	if apiID == "" || strings.TrimSpace(route.RouteID) == "" {
		return nil
	}
	relationships := []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAPIGatewayV2APIHasRoute,
		SourceResourceID: apiID,
		SourceARN:        apiARN(boundary.Region, apiID),
		TargetResourceID: routeID,
		TargetType:       awscloud.ResourceTypeAPIGatewayV2Route,
		Attributes: map[string]any{
			"route_key": strings.TrimSpace(route.RouteKey),
		},
		SourceRecordID: apiID + "#route#" + routeID,
	}}
	if integrationID := integrationTargetFromRoute(route.Target); integrationID != "" {
		integrationRes := integrationResourceID(route.APIID, integrationID)
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2RouteUsesIntegration,
			SourceResourceID: routeID,
			TargetResourceID: integrationRes,
			TargetType:       awscloud.ResourceTypeAPIGatewayV2Integration,
			Attributes: map[string]any{
				"route_key":      strings.TrimSpace(route.RouteKey),
				"integration_id": integrationID,
			},
			SourceRecordID: routeID + "#uses#" + integrationRes,
		})
	}
	return relationships
}

// integrationRelationships records the backing target an integration dispatches
// to: a Lambda function (joined by function ARN), an external HTTP endpoint, or
// a VPC link for private integrations.
func integrationRelationships(boundary awscloud.Boundary, api API, integration Integration) []awscloud.RelationshipObservation {
	integrationRes := integrationResourceID(integration.APIID, integration.IntegrationID)
	if strings.TrimSpace(integration.IntegrationID) == "" {
		return nil
	}
	_ = api
	var relationships []awscloud.RelationshipObservation
	if lambdaARN := lambdaARNFromIntegrationURI(integration.URI); lambdaARN != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2IntegrationTargetsLambda,
			SourceResourceID: integrationRes,
			TargetResourceID: lambdaARN,
			TargetARN:        lambdaARN,
			TargetType:       awscloud.ResourceTypeLambdaFunction,
			Attributes: map[string]any{
				"integration_type": strings.TrimSpace(integration.Type),
			},
			SourceRecordID: integrationRes + "#targets-lambda#" + lambdaARN,
		})
	} else if endpoint := httpEndpointFromIntegrationURI(integration.URI); endpoint != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2IntegrationTargetsHTTP,
			SourceResourceID: integrationRes,
			TargetResourceID: endpoint,
			TargetType:       awscloud.APIGatewayV2HTTPEndpointTargetType,
			Attributes: map[string]any{
				"integration_type": strings.TrimSpace(integration.Type),
			},
			SourceRecordID: integrationRes + "#targets-http#" + endpoint,
		})
	}
	if linkID := strings.TrimSpace(integration.ConnectionID); linkID != "" &&
		strings.EqualFold(strings.TrimSpace(integration.ConnectionType), "VPC_LINK") {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2IntegrationUsesVPCLink,
			SourceResourceID: integrationRes,
			TargetResourceID: linkID,
			TargetType:       awscloud.ResourceTypeAPIGatewayV2VPCLink,
			Attributes: map[string]any{
				"integration_type": strings.TrimSpace(integration.Type),
			},
			SourceRecordID: integrationRes + "#uses-vpc-link#" + linkID,
		})
	}
	return relationships
}

// vpcLinkRelationships records the subnets and security groups a VPC link spans,
// joined by the bare subnet and group ids the EC2 scanner publishes.
func vpcLinkRelationships(boundary awscloud.Boundary, link VPCLink) []awscloud.RelationshipObservation {
	linkID := strings.TrimSpace(link.ID)
	if linkID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	for _, subnetID := range cloneStrings(link.SubnetIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2VPCLinkUsesSubnet,
			SourceResourceID: linkID,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   linkID + "#subnet#" + subnetID,
		})
	}
	for _, groupID := range cloneStrings(link.SecurityGroupIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2VPCLinkUsesSecurityGroup,
			SourceResourceID: linkID,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   linkID + "#security-group#" + groupID,
		})
	}
	return relationships
}

// domainRelationships records the custom-domain ACM certificate dependencies and
// the API mappings a custom domain routes to.
func domainRelationships(boundary awscloud.Boundary, domain DomainName) []awscloud.RelationshipObservation {
	sourceID := domainResourceID(domain)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	for _, certificateARN := range cloneStrings(domain.CertificateARNs) {
		if !isARN(certificateARN) {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2DomainUsesACMCertificate,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(domain.ARN),
			TargetResourceID: certificateARN,
			TargetARN:        certificateARN,
			TargetType:       awscloud.ResourceTypeACMCertificate,
			Attributes: map[string]any{
				"domain": strings.TrimSpace(domain.Name),
			},
			SourceRecordID: sourceID + "#acm#" + certificateARN,
		})
	}
	for _, group := range groupedMappings(domain.Mappings) {
		if group.apiID == "" {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAPIGatewayV2DomainMapsToAPI,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(domain.ARN),
			TargetResourceID: group.apiID,
			TargetARN:        apiARN(boundary.Region, group.apiID),
			TargetType:       awscloud.ResourceTypeAPIGatewayV2API,
			Attributes: map[string]any{
				"mappings": group.attributes,
			},
			SourceRecordID: sourceID + "#maps-to#" + group.apiID,
		})
	}
	return relationships
}

type mappingGroup struct {
	apiID      string
	attributes []map[string]any
}

// groupedMappings collapses a domain's API mappings by target API id so one
// domain-to-API edge carries all the keys/stages for that API.
func groupedMappings(mappings []Mapping) []mappingGroup {
	groups := make(map[string]mappingGroup)
	for _, mapping := range mappings {
		apiID := strings.TrimSpace(mapping.APIID)
		if apiID == "" {
			continue
		}
		group := groups[apiID]
		group.apiID = apiID
		attributes := map[string]any{
			"domain": strings.TrimSpace(mapping.Domain),
			"key":    strings.TrimSpace(mapping.Key),
			"stage":  strings.TrimSpace(mapping.Stage),
		}
		if id := strings.TrimSpace(mapping.ID); id != "" {
			attributes["id"] = id
		}
		group.attributes = append(group.attributes, attributes)
		groups[apiID] = group
	}
	output := make([]mappingGroup, 0, len(groups))
	for _, group := range groups {
		sortMappingAttributes(group.attributes)
		output = append(output, group)
	}
	sort.Slice(output, func(i, j int) bool {
		return output[i].apiID < output[j].apiID
	})
	return output
}

func sortMappingAttributes(attributes []map[string]any) {
	sort.Slice(attributes, func(i, j int) bool {
		left := attributes[i]
		right := attributes[j]
		for _, key := range []string{"key", "stage", "domain", "id"} {
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

// mappingAttributes renders a domain's API mappings as a stable attribute slice
// for the domain resource fact.
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
			"api_id": strings.TrimSpace(mapping.APIID),
			"key":    strings.TrimSpace(mapping.Key),
			"stage":  strings.TrimSpace(mapping.Stage),
		}
		if id := strings.TrimSpace(mapping.ID); id != "" {
			attributes["id"] = id
		}
		output = append(output, attributes)
	}
	if len(output) == 0 {
		return nil
	}
	sort.Slice(output, func(i, j int) bool {
		if stringAttribute(output[i], "api_id") != stringAttribute(output[j], "api_id") {
			return stringAttribute(output[i], "api_id") < stringAttribute(output[j], "api_id")
		}
		return stringAttribute(output[i], "key") < stringAttribute(output[j], "key")
	})
	return output
}
