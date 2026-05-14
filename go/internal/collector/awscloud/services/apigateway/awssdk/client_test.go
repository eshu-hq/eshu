package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigateway "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
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
				Id:                        aws.String(restAPIID),
				Name:                      aws.String("orders-rest"),
				Description:               aws.String("orders REST API"),
				CreatedDate:               aws.Time(created),
				Version:                   aws.String("v1"),
				ApiStatus:                 awsapigatewaytypes.ApiStatusAvailable,
				ApiKeySource:              awsapigatewaytypes.ApiKeySourceTypeHeader,
				DisableExecuteApiEndpoint: true,
				EndpointConfiguration: &awsapigatewaytypes.EndpointConfiguration{
					Types:          []awsapigatewaytypes.EndpointType{awsapigatewaytypes.EndpointTypeRegional},
					VpcEndpointIds: []string{"vpce-123"},
				},
				Policy: aws.String("should-not-persist"),
				Tags:   map[string]string{"Environment": "prod"},
			}},
		}},
		restStagePages: []*awsapigateway.GetStagesOutput{{
			Item: []awsapigatewaytypes.Stage{{
				StageName:           aws.String("prod"),
				DeploymentId:        aws.String("dep-1"),
				CreatedDate:         aws.Time(created),
				LastUpdatedDate:     aws.Time(created.Add(time.Hour)),
				CacheClusterEnabled: true,
				CacheClusterSize:    awsapigatewaytypes.CacheClusterSizeSize0Point5Gb,
				CacheClusterStatus:  awsapigatewaytypes.CacheClusterStatusAvailable,
				TracingEnabled:      true,
				AccessLogSettings: &awsapigatewaytypes.AccessLogSettings{
					DestinationArn: aws.String("arn:aws:logs:us-east-1:123456789012:log-group:/aws/apigateway/orders"),
					Format:         aws.String("$context.requestId $context.identity.sourceIp"),
				},
				Variables: map[string]string{"SECRET": "should-not-persist"},
				Tags:      map[string]string{"Stage": "prod"},
			}},
		}},
		restResourcePages: []*awsapigateway.GetResourcesOutput{{
			Items: []awsapigatewaytypes.Resource{{
				Id:   aws.String("res-1"),
				Path: aws.String("/orders"),
				ResourceMethods: map[string]awsapigatewaytypes.Method{"POST": {
					MethodIntegration: &awsapigatewaytypes.Integration{
						Type:             awsapigatewaytypes.IntegrationTypeAwsProxy,
						Uri:              aws.String("arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/" + lambdaARN + "/invocations"),
						Credentials:      aws.String("arn:aws:iam::123456789012:role/secret"),
						RequestTemplates: map[string]string{"application/json": "$input.body"},
						ConnectionType:   awsapigatewaytypes.ConnectionTypeInternet,
						TimeoutInMillis:  29000,
					},
				}},
			}},
		}},
		restDomainPages: []*awsapigateway.GetDomainNamesOutput{{
			Items: []awsapigatewaytypes.DomainName{{
				DomainName:                          aws.String("api.example.com"),
				DomainNameArn:                       aws.String(restDomainARN),
				RegionalCertificateArn:              aws.String(certificateARN),
				OwnershipVerificationCertificateArn: aws.String(certificateARN),
				RegionalDomainName:                  aws.String("d-abc.execute-api.us-east-1.amazonaws.com"),
				RegionalHostedZoneId:                aws.String("Z1UJRXOUMOOFQ8"),
				DomainNameStatus:                    awsapigatewaytypes.DomainNameStatusAvailable,
				EndpointConfiguration: &awsapigatewaytypes.EndpointConfiguration{
					Types: []awsapigatewaytypes.EndpointType{awsapigatewaytypes.EndpointTypeRegional},
				},
				Policy:           aws.String("should-not-persist"),
				ManagementPolicy: aws.String("should-not-persist"),
				Tags:             map[string]string{"Domain": "orders"},
			}},
		}},
		restMappingPages: []*awsapigateway.GetBasePathMappingsOutput{{
			Items: []awsapigatewaytypes.BasePathMapping{{
				BasePath:  aws.String("(none)"),
				RestApiId: aws.String(restAPIID),
				Stage:     aws.String("prod"),
			}},
		}},
	}
	v2Fake := &fakeV2API{
		v2APIPages: []*awsapigatewayv2.GetApisOutput{{
			Items: []awsapigatewayv2types.Api{{
				ApiId:                     aws.String(v2APIID),
				Name:                      aws.String("orders-http"),
				ProtocolType:              awsapigatewayv2types.ProtocolTypeHttp,
				ApiEndpoint:               aws.String("https://z9y8x7w6.execute-api.us-east-1.amazonaws.com"),
				CreatedDate:               aws.Time(created),
				DisableExecuteApiEndpoint: aws.Bool(true),
				ApiGatewayManaged:         aws.Bool(true),
				IpAddressType:             awsapigatewayv2types.IpAddressTypeDualstack,
				Tags:                      map[string]string{"Environment": "prod"},
			}},
		}},
		v2StagePages: []*awsapigatewayv2.GetStagesOutput{{
			Items: []awsapigatewayv2types.Stage{{
				StageName:      aws.String("$default"),
				DeploymentId:   aws.String("dep-v2"),
				AutoDeploy:     aws.Bool(true),
				StageVariables: map[string]string{"SECRET": "should-not-persist"},
			}},
		}},
		v2IntegrationPages: []*awsapigatewayv2.GetIntegrationsOutput{{
			Items: []awsapigatewayv2types.Integration{{
				IntegrationId:        aws.String("int-1"),
				IntegrationMethod:    aws.String("POST"),
				IntegrationType:      awsapigatewayv2types.IntegrationTypeAwsProxy,
				IntegrationUri:       aws.String(lambdaARN),
				CredentialsArn:       aws.String("arn:aws:iam::123456789012:role/secret"),
				PayloadFormatVersion: aws.String("2.0"),
				TimeoutInMillis:      aws.Int32(30000),
			}},
		}},
		v2DomainPages: []*awsapigatewayv2.GetDomainNamesOutput{{
			Items: []awsapigatewayv2types.DomainName{{
				DomainName:                    aws.String("http.example.com"),
				DomainNameArn:                 aws.String("arn:aws:apigateway:us-east-1::/domainnames/http.example.com"),
				ApiMappingSelectionExpression: aws.String("$request.basepath"),
				DomainNameConfigurations: []awsapigatewayv2types.DomainNameConfiguration{{
					CertificateArn: aws.String(certificateARN),
					EndpointType:   awsapigatewayv2types.EndpointTypeRegional,
					SecurityPolicy: awsapigatewayv2types.SecurityPolicyTls12,
				}},
			}},
		}},
		v2MappingPages: []*awsapigatewayv2.GetApiMappingsOutput{{
			Items: []awsapigatewayv2types.ApiMapping{{
				ApiMappingId:  aws.String("map-1"),
				ApiMappingKey: aws.String("orders"),
				ApiId:         aws.String(v2APIID),
				Stage:         aws.String("$default"),
			}},
		}},
	}
	adapter := &Client{
		rest:     restFake,
		v2:       v2Fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceAPIGateway},
	}

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.RESTAPIs), 1; got != want {
		t.Fatalf("len(RESTAPIs) = %d, want %d", got, want)
	}
	restAPI := snapshot.RESTAPIs[0]
	if restAPI.Policy != "" {
		t.Fatalf("REST policy persisted = %q, want empty", restAPI.Policy)
	}
	if got, want := len(restAPI.Stages), 1; got != want {
		t.Fatalf("len(REST stages) = %d, want %d", got, want)
	}
	if restAPI.Stages[0].StageVariables != nil {
		t.Fatalf("REST stage variables persisted = %#v, want nil", restAPI.Stages[0].StageVariables)
	}
	if got, want := restAPI.Integrations[0].URI, "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/"+lambdaARN+"/invocations"; got != want {
		t.Fatalf("REST integration URI = %q, want %q", got, want)
	}
	if restAPI.Integrations[0].CredentialsARN != "" {
		t.Fatalf("REST integration credentials persisted = %q, want empty", restAPI.Integrations[0].CredentialsARN)
	}
	if got, want := len(snapshot.V2APIs), 1; got != want {
		t.Fatalf("len(V2APIs) = %d, want %d", got, want)
	}
	if snapshot.V2APIs[0].Stages[0].StageVariables != nil {
		t.Fatalf("v2 stage variables persisted = %#v, want nil", snapshot.V2APIs[0].Stages[0].StageVariables)
	}
	if snapshot.V2APIs[0].Integrations[0].CredentialsARN != "" {
		t.Fatalf("v2 integration credentials persisted = %q, want empty", snapshot.V2APIs[0].Integrations[0].CredentialsARN)
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
	restAPIPages      []*awsapigateway.GetRestApisOutput
	restAPICalls      int
	restStagePages    []*awsapigateway.GetStagesOutput
	restStageCalls    int
	restResourcePages []*awsapigateway.GetResourcesOutput
	restResourceCalls int
	restResourceEmbed []string
	restDomainPages   []*awsapigateway.GetDomainNamesOutput
	restDomainCalls   int
	restMappingPages  []*awsapigateway.GetBasePathMappingsOutput
	restMappingCalls  int
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

var _ restAPIClient = (*fakeRESTAPI)(nil)
var _ v2APIClient = (*fakeV2API)(nil)
