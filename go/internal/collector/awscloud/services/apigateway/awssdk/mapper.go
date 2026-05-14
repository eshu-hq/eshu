package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigateway"
)

func mapRESTAPI(item awsapigatewaytypes.RestApi) apigatewayservice.RESTAPI {
	endpointTypes, vpcEndpointIDs := mapEndpointConfiguration(item.EndpointConfiguration)
	return apigatewayservice.RESTAPI{
		ID:                        strings.TrimSpace(aws.ToString(item.Id)),
		Name:                      strings.TrimSpace(aws.ToString(item.Name)),
		Description:               strings.TrimSpace(aws.ToString(item.Description)),
		CreatedDate:               aws.ToTime(item.CreatedDate),
		Version:                   strings.TrimSpace(aws.ToString(item.Version)),
		APIStatus:                 string(item.ApiStatus),
		APIKeySource:              string(item.ApiKeySource),
		DisableExecuteAPIEndpoint: item.DisableExecuteApiEndpoint,
		EndpointTypes:             endpointTypes,
		VPCEndpointIDs:            vpcEndpointIDs,
		Tags:                      cloneStringMap(item.Tags),
	}
}

func mapRESTStage(apiID string, item awsapigatewaytypes.Stage) apigatewayservice.Stage {
	return apigatewayservice.Stage{
		APIKind:              apigatewayservice.APIKindREST,
		APIID:                strings.TrimSpace(apiID),
		Name:                 strings.TrimSpace(aws.ToString(item.StageName)),
		DeploymentID:         strings.TrimSpace(aws.ToString(item.DeploymentId)),
		Description:          strings.TrimSpace(aws.ToString(item.Description)),
		CreatedDate:          aws.ToTime(item.CreatedDate),
		LastUpdatedDate:      aws.ToTime(item.LastUpdatedDate),
		CacheClusterEnabled:  item.CacheClusterEnabled,
		CacheClusterSize:     string(item.CacheClusterSize),
		CacheClusterStatus:   string(item.CacheClusterStatus),
		TracingEnabled:       item.TracingEnabled,
		ClientCertificateID:  strings.TrimSpace(aws.ToString(item.ClientCertificateId)),
		AccessLogDestination: accessLogDestinationREST(item.AccessLogSettings),
		WebACLARN:            strings.TrimSpace(aws.ToString(item.WebAclArn)),
		Tags:                 cloneStringMap(item.Tags),
	}
}

func mapRESTResourceIntegrations(
	apiID string,
	resource awsapigatewaytypes.Resource,
) []apigatewayservice.Integration {
	if len(resource.ResourceMethods) == 0 {
		return nil
	}
	output := make([]apigatewayservice.Integration, 0, len(resource.ResourceMethods))
	for method, item := range resource.ResourceMethods {
		if item.MethodIntegration == nil {
			continue
		}
		integration := item.MethodIntegration
		output = append(output, apigatewayservice.Integration{
			APIKind:        apigatewayservice.APIKindREST,
			APIID:          strings.TrimSpace(apiID),
			ResourceID:     strings.TrimSpace(aws.ToString(resource.Id)),
			ResourcePath:   strings.TrimSpace(aws.ToString(resource.Path)),
			Method:         strings.TrimSpace(method),
			Type:           string(integration.Type),
			URI:            strings.TrimSpace(aws.ToString(integration.Uri)),
			ConnectionType: string(integration.ConnectionType),
			ConnectionID:   strings.TrimSpace(aws.ToString(integration.ConnectionId)),
			TimeoutMillis:  integration.TimeoutInMillis,
		})
	}
	return output
}

func mapRESTDomain(item awsapigatewaytypes.DomainName) apigatewayservice.DomainName {
	endpointTypes, _ := mapEndpointConfiguration(item.EndpointConfiguration)
	return apigatewayservice.DomainName{
		APIKind:          apigatewayservice.APIKindREST,
		Name:             strings.TrimSpace(aws.ToString(item.DomainName)),
		ARN:              strings.TrimSpace(aws.ToString(item.DomainNameArn)),
		Status:           string(item.DomainNameStatus),
		EndpointTypes:    endpointTypes,
		RegionalDomain:   strings.TrimSpace(aws.ToString(item.RegionalDomainName)),
		RegionalZoneID:   strings.TrimSpace(aws.ToString(item.RegionalHostedZoneId)),
		DistributionName: strings.TrimSpace(aws.ToString(item.DistributionDomainName)),
		DistributionZone: strings.TrimSpace(aws.ToString(item.DistributionHostedZoneId)),
		CertificateARNs: cloneStrings([]string{
			aws.ToString(item.CertificateArn),
			aws.ToString(item.RegionalCertificateArn),
			aws.ToString(item.OwnershipVerificationCertificateArn),
		}),
		SecurityPolicy: string(item.SecurityPolicy),
		Tags:           cloneStringMap(item.Tags),
	}
}

func mapRESTMapping(domainName string, item awsapigatewaytypes.BasePathMapping) apigatewayservice.Mapping {
	return apigatewayservice.Mapping{
		APIKind: apigatewayservice.APIKindREST,
		Domain:  strings.TrimSpace(domainName),
		Key:     strings.TrimSpace(aws.ToString(item.BasePath)),
		APIID:   strings.TrimSpace(aws.ToString(item.RestApiId)),
		Stage:   strings.TrimSpace(aws.ToString(item.Stage)),
	}
}

func mapV2API(item awsapigatewayv2types.Api) apigatewayservice.V2API {
	return apigatewayservice.V2API{
		ID:                        strings.TrimSpace(aws.ToString(item.ApiId)),
		Name:                      strings.TrimSpace(aws.ToString(item.Name)),
		ProtocolType:              string(item.ProtocolType),
		Endpoint:                  strings.TrimSpace(aws.ToString(item.ApiEndpoint)),
		CreatedDate:               aws.ToTime(item.CreatedDate),
		Description:               strings.TrimSpace(aws.ToString(item.Description)),
		DisableExecuteAPIEndpoint: aws.ToBool(item.DisableExecuteApiEndpoint),
		APIGatewayManaged:         cloneBool(item.ApiGatewayManaged),
		IPAddressType:             string(item.IpAddressType),
		Tags:                      cloneStringMap(item.Tags),
	}
}

func mapV2Stage(apiID string, item awsapigatewayv2types.Stage) apigatewayservice.Stage {
	return apigatewayservice.Stage{
		APIKind:              apigatewayservice.APIKindV2,
		APIID:                strings.TrimSpace(apiID),
		Name:                 strings.TrimSpace(aws.ToString(item.StageName)),
		DeploymentID:         strings.TrimSpace(aws.ToString(item.DeploymentId)),
		Description:          strings.TrimSpace(aws.ToString(item.Description)),
		CreatedDate:          aws.ToTime(item.CreatedDate),
		LastUpdatedDate:      aws.ToTime(item.LastUpdatedDate),
		ClientCertificateID:  strings.TrimSpace(aws.ToString(item.ClientCertificateId)),
		AccessLogDestination: accessLogDestinationV2(item.AccessLogSettings),
		AutoDeploy:           cloneBool(item.AutoDeploy),
		APIGatewayManaged:    cloneBool(item.ApiGatewayManaged),
		Tags:                 cloneStringMap(item.Tags),
	}
}

func mapV2Integration(apiID string, item awsapigatewayv2types.Integration) apigatewayservice.Integration {
	return apigatewayservice.Integration{
		APIKind:              apigatewayservice.APIKindV2,
		APIID:                strings.TrimSpace(apiID),
		IntegrationID:        strings.TrimSpace(aws.ToString(item.IntegrationId)),
		Method:               strings.TrimSpace(aws.ToString(item.IntegrationMethod)),
		Type:                 string(item.IntegrationType),
		URI:                  strings.TrimSpace(aws.ToString(item.IntegrationUri)),
		ConnectionType:       string(item.ConnectionType),
		ConnectionID:         strings.TrimSpace(aws.ToString(item.ConnectionId)),
		PayloadFormatVersion: strings.TrimSpace(aws.ToString(item.PayloadFormatVersion)),
		TimeoutMillis:        aws.ToInt32(item.TimeoutInMillis),
		APIGatewayManaged:    cloneBool(item.ApiGatewayManaged),
	}
}

func mapV2Domain(item awsapigatewayv2types.DomainName) apigatewayservice.DomainName {
	return apigatewayservice.DomainName{
		APIKind:          apigatewayservice.APIKindV2,
		Name:             strings.TrimSpace(aws.ToString(item.DomainName)),
		ARN:              strings.TrimSpace(aws.ToString(item.DomainNameArn)),
		Status:           firstV2DomainStatus(item.DomainNameConfigurations),
		APIMappingSelect: strings.TrimSpace(aws.ToString(item.ApiMappingSelectionExpression)),
		EndpointTypes:    mapV2DomainEndpointTypes(item.DomainNameConfigurations),
		RegionalDomain:   firstV2APIGatewayDomainName(item.DomainNameConfigurations),
		RegionalZoneID:   firstV2HostedZoneID(item.DomainNameConfigurations),
		CertificateARNs:  mapV2DomainCertificateARNs(item.DomainNameConfigurations),
		SecurityPolicy:   firstV2SecurityPolicy(item.DomainNameConfigurations),
		Tags:             cloneStringMap(item.Tags),
	}
}

func mapV2Mapping(domainName string, item awsapigatewayv2types.ApiMapping) apigatewayservice.Mapping {
	return apigatewayservice.Mapping{
		APIKind: apigatewayservice.APIKindV2,
		Domain:  strings.TrimSpace(domainName),
		ID:      strings.TrimSpace(aws.ToString(item.ApiMappingId)),
		Key:     strings.TrimSpace(aws.ToString(item.ApiMappingKey)),
		APIID:   strings.TrimSpace(aws.ToString(item.ApiId)),
		Stage:   strings.TrimSpace(aws.ToString(item.Stage)),
	}
}
