// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsapigateway "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientSnapshotReadsRESTAndV2MetadataOnly(t *testing.T) {
	created := time.Date(2026, 5, 14, 18, 0, 0, 0, time.UTC)
	restAPIID := "a1b2c3d4"
	v2APIID := "z9y8x7w6"
	restDomainARN := "arn:aws:apigateway:us-east-1::/domainnames/api.example.com"
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/cert-1"
	lambdaARN := "arn:aws:lambda:us-east-1:123456789012:function:orders"
	restFake := &fakeRESTAPI{
		restAPIPages: []*awsapigateway.GetRestApisOutput{{
			Items: []awsapigatewaytypes.RestApi{{
				Id:                        awsv2.String(restAPIID),
				Name:                      awsv2.String("orders-rest"),
				Description:               awsv2.String("orders REST API"),
				CreatedDate:               awsv2.Time(created),
				Version:                   awsv2.String("v1"),
				ApiStatus:                 awsapigatewaytypes.ApiStatusAvailable,
				ApiKeySource:              awsapigatewaytypes.ApiKeySourceTypeHeader,
				DisableExecuteApiEndpoint: true,
				EndpointConfiguration: &awsapigatewaytypes.EndpointConfiguration{
					Types:          []awsapigatewaytypes.EndpointType{awsapigatewaytypes.EndpointTypeRegional},
					VpcEndpointIds: []string{"vpce-123"},
				},
				Policy: awsv2.String("should-not-persist"),
				Tags:   map[string]string{"Environment": "prod"},
			}},
		}},
		restStagePages: []*awsapigateway.GetStagesOutput{{
			Item: []awsapigatewaytypes.Stage{{
				StageName:           awsv2.String("prod"),
				DeploymentId:        awsv2.String("dep-1"),
				CreatedDate:         awsv2.Time(created),
				LastUpdatedDate:     awsv2.Time(created.Add(time.Hour)),
				CacheClusterEnabled: true,
				CacheClusterSize:    awsapigatewaytypes.CacheClusterSizeSize0Point5Gb,
				CacheClusterStatus:  awsapigatewaytypes.CacheClusterStatusAvailable,
				TracingEnabled:      true,
				AccessLogSettings: &awsapigatewaytypes.AccessLogSettings{
					DestinationArn: awsv2.String("arn:aws:logs:us-east-1:123456789012:log-group:/aws/apigateway/orders"),
					Format:         awsv2.String("$context.requestId $context.identity.sourceIp"),
				},
				Variables: map[string]string{"SECRET": "should-not-persist"},
				Tags:      map[string]string{"Stage": "prod"},
			}},
		}},
		restResourcePages: []*awsapigateway.GetResourcesOutput{{
			Items: []awsapigatewaytypes.Resource{{
				Id:   awsv2.String("res-1"),
				Path: awsv2.String("/orders"),
				ResourceMethods: map[string]awsapigatewaytypes.Method{"POST": {
					MethodIntegration: &awsapigatewaytypes.Integration{
						Type:             awsapigatewaytypes.IntegrationTypeAwsProxy,
						Uri:              awsv2.String("arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/" + lambdaARN + "/invocations"),
						Credentials:      awsv2.String("arn:aws:iam::123456789012:role/secret"),
						RequestTemplates: map[string]string{"application/json": "$input.body"},
						ConnectionType:   awsapigatewaytypes.ConnectionTypeInternet,
						TimeoutInMillis:  29000,
					},
				}},
			}},
		}},
		restDomainPages: []*awsapigateway.GetDomainNamesOutput{{
			Items: []awsapigatewaytypes.DomainName{{
				DomainName:                          awsv2.String("api.example.com"),
				DomainNameArn:                       awsv2.String(restDomainARN),
				RegionalCertificateArn:              awsv2.String(certificateARN),
				OwnershipVerificationCertificateArn: awsv2.String(certificateARN),
				RegionalDomainName:                  awsv2.String("d-abc.execute-api.us-east-1.amazonaws.com"),
				RegionalHostedZoneId:                awsv2.String("Z1UJRXOUMOOFQ8"),
				DomainNameStatus:                    awsapigatewaytypes.DomainNameStatusAvailable,
				EndpointConfiguration: &awsapigatewaytypes.EndpointConfiguration{
					Types: []awsapigatewaytypes.EndpointType{awsapigatewaytypes.EndpointTypeRegional},
				},
				Policy:           awsv2.String("should-not-persist"),
				ManagementPolicy: awsv2.String("should-not-persist"),
				Tags:             map[string]string{"Domain": "orders"},
			}},
		}},
		restMappingPages: []*awsapigateway.GetBasePathMappingsOutput{{
			Items: []awsapigatewaytypes.BasePathMapping{{
				BasePath:  awsv2.String("(none)"),
				RestApiId: awsv2.String(restAPIID),
				Stage:     awsv2.String("prod"),
			}},
		}},
	}
	v2Fake := &fakeV2API{
		v2APIPages: []*awsapigatewayv2.GetApisOutput{{
			Items: []awsapigatewayv2types.Api{{
				ApiId:                     awsv2.String(v2APIID),
				Name:                      awsv2.String("orders-http"),
				ProtocolType:              awsapigatewayv2types.ProtocolTypeHttp,
				ApiEndpoint:               awsv2.String("https://z9y8x7w6.execute-api.us-east-1.amazonaws.com"),
				CreatedDate:               awsv2.Time(created),
				DisableExecuteApiEndpoint: awsv2.Bool(true),
				ApiGatewayManaged:         awsv2.Bool(true),
				IpAddressType:             awsapigatewayv2types.IpAddressTypeDualstack,
				Tags:                      map[string]string{"Environment": "prod"},
			}},
		}},
		v2StagePages: []*awsapigatewayv2.GetStagesOutput{{
			Items: []awsapigatewayv2types.Stage{{
				StageName:      awsv2.String("$default"),
				DeploymentId:   awsv2.String("dep-v2"),
				AutoDeploy:     awsv2.Bool(true),
				StageVariables: map[string]string{"SECRET": "should-not-persist"},
			}},
		}},
		v2IntegrationPages: []*awsapigatewayv2.GetIntegrationsOutput{{
			Items: []awsapigatewayv2types.Integration{{
				IntegrationId:        awsv2.String("int-1"),
				IntegrationMethod:    awsv2.String("POST"),
				IntegrationType:      awsapigatewayv2types.IntegrationTypeAwsProxy,
				IntegrationUri:       awsv2.String(lambdaARN),
				CredentialsArn:       awsv2.String("arn:aws:iam::123456789012:role/secret"),
				PayloadFormatVersion: awsv2.String("2.0"),
				TimeoutInMillis:      awsv2.Int32(30000),
			}},
		}},
		v2DomainPages: []*awsapigatewayv2.GetDomainNamesOutput{{
			Items: []awsapigatewayv2types.DomainName{{
				DomainName:                    awsv2.String("http.example.com"),
				DomainNameArn:                 awsv2.String("arn:aws:apigateway:us-east-1::/domainnames/http.example.com"),
				ApiMappingSelectionExpression: awsv2.String("$request.basepath"),
				DomainNameConfigurations: []awsapigatewayv2types.DomainNameConfiguration{{
					CertificateArn: awsv2.String(certificateARN),
					EndpointType:   awsapigatewayv2types.EndpointTypeRegional,
					SecurityPolicy: awsapigatewayv2types.SecurityPolicyTls12,
				}},
			}},
		}},
		v2MappingPages: []*awsapigatewayv2.GetApiMappingsOutput{{
			Items: []awsapigatewayv2types.ApiMapping{{
				ApiMappingId:  awsv2.String("map-1"),
				ApiMappingKey: awsv2.String("orders"),
				ApiId:         awsv2.String(v2APIID),
				Stage:         awsv2.String("$default"),
			}},
		}},
	}
	adapter := &Client{
		rest:     restFake,
		v2:       v2Fake,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceAPIGateway},
	}

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.RESTAPIs), 1; got != want {
		t.Fatalf("len(RESTAPIs) = %d, want %d", got, want)
	}
	restAPI := snapshot.RESTAPIs[0]
	if got, want := len(restAPI.Stages), 1; got != want {
		t.Fatalf("len(REST stages) = %d, want %d", got, want)
	}
	if got, want := restAPI.Integrations[0].URI, "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/"+lambdaARN+"/invocations"; got != want {
		t.Fatalf("REST integration URI = %q, want %q", got, want)
	}
	if got, want := len(snapshot.V2APIs), 1; got != want {
		t.Fatalf("len(V2APIs) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Domains), 2; got != want {
		t.Fatalf("len(Domains) = %d, want %d", got, want)
	}
	if got, want := restFake.restAPICalls, 1; got != want {
		t.Fatalf("GetRestApis calls = %d, want %d", got, want)
	}
	if got, want := restFake.restResourceEmbed, []string{"methods"}; !reflectDeepEqualStrings(got, want) {
		t.Fatalf("GetResources embed = %#v, want %#v", got, want)
	}
}

func reflectDeepEqualStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type fakeRESTAPI struct {
	restAPIPages       []*awsapigateway.GetRestApisOutput
	restAPICalls       int
	restStagePages     []*awsapigateway.GetStagesOutput
	restStageCalls     int
	restResourcePages  []*awsapigateway.GetResourcesOutput
	restResourceErrors []error
	restResourceCalls  int
	restResourceEmbed  []string
	restDomainPages    []*awsapigateway.GetDomainNamesOutput
	restDomainCalls    int
	restMappingPages   []*awsapigateway.GetBasePathMappingsOutput
	restMappingCalls   int
}

func (f *fakeRESTAPI) GetRestApis(
	context.Context,
	*awsapigateway.GetRestApisInput,
	...func(*awsapigateway.Options),
) (*awsapigateway.GetRestApisOutput, error) {
	if f.restAPICalls >= len(f.restAPIPages) {
		return &awsapigateway.GetRestApisOutput{}, nil
	}
	page := f.restAPIPages[f.restAPICalls]
	f.restAPICalls++
	return page, nil
}

func (f *fakeRESTAPI) GetStages(
	context.Context,
	*awsapigateway.GetStagesInput,
	...func(*awsapigateway.Options),
) (*awsapigateway.GetStagesOutput, error) {
	if f.restStageCalls >= len(f.restStagePages) {
		return &awsapigateway.GetStagesOutput{}, nil
	}
	page := f.restStagePages[f.restStageCalls]
	f.restStageCalls++
	return page, nil
}

func (f *fakeRESTAPI) GetResources(
	_ context.Context,
	input *awsapigateway.GetResourcesInput,
	_ ...func(*awsapigateway.Options),
) (*awsapigateway.GetResourcesOutput, error) {
	f.restResourceEmbed = append([]string(nil), input.Embed...)
	if f.restResourceCalls < len(f.restResourceErrors) && f.restResourceErrors[f.restResourceCalls] != nil {
		err := f.restResourceErrors[f.restResourceCalls]
		f.restResourceCalls++
		return nil, err
	}
	if f.restResourceCalls >= len(f.restResourcePages) {
		return &awsapigateway.GetResourcesOutput{}, nil
	}
	page := f.restResourcePages[f.restResourceCalls]
	f.restResourceCalls++
	return page, nil
}

func (f *fakeRESTAPI) GetDomainNames(
	context.Context,
	*awsapigateway.GetDomainNamesInput,
	...func(*awsapigateway.Options),
) (*awsapigateway.GetDomainNamesOutput, error) {
	if f.restDomainCalls >= len(f.restDomainPages) {
		return &awsapigateway.GetDomainNamesOutput{}, nil
	}
	page := f.restDomainPages[f.restDomainCalls]
	f.restDomainCalls++
	return page, nil
}

func (f *fakeRESTAPI) GetBasePathMappings(
	context.Context,
	*awsapigateway.GetBasePathMappingsInput,
	...func(*awsapigateway.Options),
) (*awsapigateway.GetBasePathMappingsOutput, error) {
	if f.restMappingCalls >= len(f.restMappingPages) {
		return &awsapigateway.GetBasePathMappingsOutput{}, nil
	}
	page := f.restMappingPages[f.restMappingCalls]
	f.restMappingCalls++
	return page, nil
}

type fakeV2API struct {
	v2APIPages         []*awsapigatewayv2.GetApisOutput
	v2APICalls         int
	v2StagePages       []*awsapigatewayv2.GetStagesOutput
	v2StageCalls       int
	v2IntegrationPages []*awsapigatewayv2.GetIntegrationsOutput
	v2IntegrationCalls int
	v2DomainPages      []*awsapigatewayv2.GetDomainNamesOutput
	v2DomainCalls      int
	v2MappingPages     []*awsapigatewayv2.GetApiMappingsOutput
	v2MappingCalls     int
}

func (f *fakeV2API) GetApis(
	context.Context,
	*awsapigatewayv2.GetApisInput,
	...func(*awsapigatewayv2.Options),
) (*awsapigatewayv2.GetApisOutput, error) {
	if f.v2APICalls >= len(f.v2APIPages) {
		return &awsapigatewayv2.GetApisOutput{}, nil
	}
	page := f.v2APIPages[f.v2APICalls]
	f.v2APICalls++
	return page, nil
}

func (f *fakeV2API) GetStages(
	context.Context,
	*awsapigatewayv2.GetStagesInput,
	...func(*awsapigatewayv2.Options),
) (*awsapigatewayv2.GetStagesOutput, error) {
	if f.v2StageCalls >= len(f.v2StagePages) {
		return &awsapigatewayv2.GetStagesOutput{}, nil
	}
	page := f.v2StagePages[f.v2StageCalls]
	f.v2StageCalls++
	return page, nil
}

func (f *fakeV2API) GetIntegrations(
	context.Context,
	*awsapigatewayv2.GetIntegrationsInput,
	...func(*awsapigatewayv2.Options),
) (*awsapigatewayv2.GetIntegrationsOutput, error) {
	if f.v2IntegrationCalls >= len(f.v2IntegrationPages) {
		return &awsapigatewayv2.GetIntegrationsOutput{}, nil
	}
	page := f.v2IntegrationPages[f.v2IntegrationCalls]
	f.v2IntegrationCalls++
	return page, nil
}

func (f *fakeV2API) GetDomainNames(
	context.Context,
	*awsapigatewayv2.GetDomainNamesInput,
	...func(*awsapigatewayv2.Options),
) (*awsapigatewayv2.GetDomainNamesOutput, error) {
	if f.v2DomainCalls >= len(f.v2DomainPages) {
		return &awsapigatewayv2.GetDomainNamesOutput{}, nil
	}
	page := f.v2DomainPages[f.v2DomainCalls]
	f.v2DomainCalls++
	return page, nil
}

func (f *fakeV2API) GetApiMappings(
	context.Context,
	*awsapigatewayv2.GetApiMappingsInput,
	...func(*awsapigatewayv2.Options),
) (*awsapigatewayv2.GetApiMappingsOutput, error) {
	if f.v2MappingCalls >= len(f.v2MappingPages) {
		return &awsapigatewayv2.GetApiMappingsOutput{}, nil
	}
	page := f.v2MappingPages[f.v2MappingCalls]
	f.v2MappingCalls++
	return page, nil
}

var (
	_ restAPIClient = (*fakeRESTAPI)(nil)
	_ v2APIClient   = (*fakeV2API)(nil)
)
