// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	apigatewayv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigatewayv2"
)

func mapAPI(item awsapigatewayv2types.Api) apigatewayv2service.API {
	return apigatewayv2service.API{
		ID:                        strings.TrimSpace(aws.ToString(item.ApiId)),
		Name:                      strings.TrimSpace(aws.ToString(item.Name)),
		ProtocolType:              string(item.ProtocolType),
		Endpoint:                  strings.TrimSpace(aws.ToString(item.ApiEndpoint)),
		CreatedDate:               aws.ToTime(item.CreatedDate),
		Description:               strings.TrimSpace(aws.ToString(item.Description)),
		Version:                   strings.TrimSpace(aws.ToString(item.Version)),
		DisableExecuteAPIEndpoint: aws.ToBool(item.DisableExecuteApiEndpoint),
		APIGatewayManaged:         cloneBool(item.ApiGatewayManaged),
		IPAddressType:             string(item.IpAddressType),
		Tags:                      cloneStringMap(item.Tags),
	}
}

func mapStage(apiID string, item awsapigatewayv2types.Stage) apigatewayv2service.Stage {
	return apigatewayv2service.Stage{
		APIID:                strings.TrimSpace(apiID),
		Name:                 strings.TrimSpace(aws.ToString(item.StageName)),
		DeploymentID:         strings.TrimSpace(aws.ToString(item.DeploymentId)),
		Description:          strings.TrimSpace(aws.ToString(item.Description)),
		CreatedDate:          aws.ToTime(item.CreatedDate),
		LastUpdatedDate:      aws.ToTime(item.LastUpdatedDate),
		AutoDeploy:           cloneBool(item.AutoDeploy),
		APIGatewayManaged:    cloneBool(item.ApiGatewayManaged),
		ClientCertificateID:  strings.TrimSpace(aws.ToString(item.ClientCertificateId)),
		AccessLogDestination: accessLogDestination(item.AccessLogSettings),
		Tags:                 cloneStringMap(item.Tags),
	}
}

// mapRoute copies only topology and authorization-summary fields. It never
// reads item.RequestModels, item.RequestParameters,
// item.ModelSelectionExpression, or item.RouteResponseSelectionExpression
// because those describe request transformation, not topology.
func mapRoute(apiID string, item awsapigatewayv2types.Route) apigatewayv2service.Route {
	return apigatewayv2service.Route{
		APIID:             strings.TrimSpace(apiID),
		RouteID:           strings.TrimSpace(aws.ToString(item.RouteId)),
		RouteKey:          strings.TrimSpace(aws.ToString(item.RouteKey)),
		Target:            strings.TrimSpace(aws.ToString(item.Target)),
		AuthorizationType: string(item.AuthorizationType),
		AuthorizerID:      strings.TrimSpace(aws.ToString(item.AuthorizerId)),
		APIKeyRequired:    cloneBool(item.ApiKeyRequired),
		APIGatewayManaged: cloneBool(item.ApiGatewayManaged),
		OperationName:     strings.TrimSpace(aws.ToString(item.OperationName)),
	}
}

// mapIntegration copies only the integration identity, type, backend
// URI/target, and connection metadata. It never reads item.RequestTemplates,
// item.RequestParameters, item.ResponseParameters,
// item.TemplateSelectionExpression, or item.CredentialsArn: the templates and
// parameters are request transformation behavior and the credential ARN is a
// secret.
func mapIntegration(apiID string, item awsapigatewayv2types.Integration) apigatewayv2service.Integration {
	return apigatewayv2service.Integration{
		APIID:                strings.TrimSpace(apiID),
		IntegrationID:        strings.TrimSpace(aws.ToString(item.IntegrationId)),
		Type:                 string(item.IntegrationType),
		Subtype:              strings.TrimSpace(aws.ToString(item.IntegrationSubtype)),
		URI:                  strings.TrimSpace(aws.ToString(item.IntegrationUri)),
		Method:               strings.TrimSpace(aws.ToString(item.IntegrationMethod)),
		ConnectionType:       string(item.ConnectionType),
		ConnectionID:         strings.TrimSpace(aws.ToString(item.ConnectionId)),
		PayloadFormatVersion: strings.TrimSpace(aws.ToString(item.PayloadFormatVersion)),
		Description:          strings.TrimSpace(aws.ToString(item.Description)),
		TimeoutMillis:        aws.ToInt32(item.TimeoutInMillis),
		APIGatewayManaged:    cloneBool(item.ApiGatewayManaged),
	}
}

// mapAuthorizer copies only the authorizer name, type, identity sources, and
// JWT issuer/audience. It never reads item.AuthorizerUri (the Lambda authorizer
// payload invocation path), item.AuthorizerCredentialsArn (the execution
// secret), or item.IdentityValidationExpression.
func mapAuthorizer(apiID string, item awsapigatewayv2types.Authorizer) apigatewayv2service.Authorizer {
	authorizer := apigatewayv2service.Authorizer{
		APIID:          strings.TrimSpace(apiID),
		AuthorizerID:   strings.TrimSpace(aws.ToString(item.AuthorizerId)),
		Name:           strings.TrimSpace(aws.ToString(item.Name)),
		Type:           string(item.AuthorizerType),
		IdentitySource: cloneStrings(item.IdentitySource),
	}
	if item.JwtConfiguration != nil {
		authorizer.JWTIssuer = strings.TrimSpace(aws.ToString(item.JwtConfiguration.Issuer))
		authorizer.JWTAudience = cloneStrings(item.JwtConfiguration.Audience)
	}
	return authorizer
}

func mapVPCLink(item awsapigatewayv2types.VpcLink) apigatewayv2service.VPCLink {
	return apigatewayv2service.VPCLink{
		ID:               strings.TrimSpace(aws.ToString(item.VpcLinkId)),
		Name:             strings.TrimSpace(aws.ToString(item.Name)),
		Status:           string(item.VpcLinkStatus),
		StatusMessage:    strings.TrimSpace(aws.ToString(item.VpcLinkStatusMessage)),
		Version:          string(item.VpcLinkVersion),
		SubnetIDs:        cloneStrings(item.SubnetIds),
		SecurityGroupIDs: cloneStrings(item.SecurityGroupIds),
		CreatedDate:      aws.ToTime(item.CreatedDate),
		Tags:             cloneStringMap(item.Tags),
	}
}

func mapDomain(item awsapigatewayv2types.DomainName) apigatewayv2service.DomainName {
	return apigatewayv2service.DomainName{
		Name:             strings.TrimSpace(aws.ToString(item.DomainName)),
		ARN:              strings.TrimSpace(aws.ToString(item.DomainNameArn)),
		Status:           firstDomainStatus(item.DomainNameConfigurations),
		APIMappingSelect: strings.TrimSpace(aws.ToString(item.ApiMappingSelectionExpression)),
		EndpointTypes:    domainEndpointTypes(item.DomainNameConfigurations),
		RegionalDomain:   firstAPIGatewayDomainName(item.DomainNameConfigurations),
		RegionalZoneID:   firstHostedZoneID(item.DomainNameConfigurations),
		CertificateARNs:  domainCertificateARNs(item.DomainNameConfigurations),
		SecurityPolicy:   firstSecurityPolicy(item.DomainNameConfigurations),
		Tags:             cloneStringMap(item.Tags),
	}
}

func mapMapping(domainName string, item awsapigatewayv2types.ApiMapping) apigatewayv2service.Mapping {
	return apigatewayv2service.Mapping{
		Domain: strings.TrimSpace(domainName),
		ID:     strings.TrimSpace(aws.ToString(item.ApiMappingId)),
		Key:    strings.TrimSpace(aws.ToString(item.ApiMappingKey)),
		APIID:  strings.TrimSpace(aws.ToString(item.ApiId)),
		Stage:  strings.TrimSpace(aws.ToString(item.Stage)),
	}
}
