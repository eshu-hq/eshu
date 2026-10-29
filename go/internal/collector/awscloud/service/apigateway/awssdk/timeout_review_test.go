// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/service/apigateway"
)

// TestSnapshotKeepsBothRESTAndV2TimeoutWarnings proves that simultaneous
// REST and v2 timeouts on the same operation leave one operator-visible
// warning per timed-out slice: the dedupe key carries api_kind so the v2
// GetStages warning no longer collapses into the REST GetStages warning.
func TestSnapshotKeepsBothRESTAndV2TimeoutWarnings(t *testing.T) {
	rest := &fakeRESTAPI{
		restAPIPages: []*awsapigateway.GetRestApisOutput{{
			Items: []awsapigatewaytypes.RestApi{{Id: aws.String("rest-1"), Name: aws.String("orders-rest")}},
		}},
		restStageErrors: []error{dialTimeoutError()},
	}
	v2 := &fakeV2API{
		v2APIPages: []*awsapigatewayv2.GetApisOutput{{
			Items: []awsapigatewayv2types.Api{{
				ApiId:        aws.String("v2-1"),
				Name:         aws.String("orders-http"),
				ProtocolType: awsapigatewayv2types.ProtocolTypeHttp,
			}},
		}},
		v2StageErrors: []error{dialTimeoutError()},
	}
	adapter := newTimeoutTestClient(rest, v2)
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (timeouts must degrade with warnings)", err)
	}
	if got, want := len(snapshot.Warnings), 2; got != want {
		t.Fatalf("len(Warnings) = %d, want %d (REST and v2 GetStages timeouts)", got, want)
	}
	kinds := map[string]bool{}
	for _, warning := range snapshot.Warnings {
		operation, _ := warning.Attributes["operation"].(string)
		if operation != "GetStages" {
			t.Fatalf("warning operation = %q, want GetStages", operation)
		}
		apiKind, _ := warning.Attributes["api_kind"].(string)
		kinds[apiKind] = true
	}
	if !kinds[apigatewayservice.APIKindREST] || !kinds[apigatewayservice.APIKindV2] {
		t.Fatalf("warning api_kinds = %v, want both rest and v2", kinds)
	}
}

// TestListRESTAPIsKeepsEarlierPagesOnTimeout proves that a timeout on a
// later page keeps the APIs and warnings already collected: the degraded
// slice carries page-one inventory plus the timeout warning instead of an
// empty slice with a lone warning.
func TestListRESTAPIsKeepsEarlierPagesOnTimeout(t *testing.T) {
	rest := &fakeRESTAPI{
		restAPIPages: []*awsapigateway.GetRestApisOutput{{
			Items:    []awsapigatewaytypes.RestApi{{Id: aws.String("rest-1"), Name: aws.String("orders-rest")}},
			Position: aws.String("page-two"),
		}},
		restAPIErrors: []error{nil, dialTimeoutError()},
	}
	adapter := newTimeoutTestClient(rest, emptyTimeoutV2())
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (timeout must degrade with warning)", err)
	}
	if got, want := len(snapshot.RESTAPIs), 1; got != want {
		t.Fatalf("len(RESTAPIs) = %d, want %d (page-one API survives the page-two timeout)", got, want)
	}
	if got := snapshot.RESTAPIs[0].ID; got != "rest-1" {
		t.Fatalf("RESTAPIs[0].ID = %q, want rest-1", got)
	}
	singleTimeoutWarning(t, snapshot, "GetRestApis")
}

// TestListV2APIsKeepsEarlierPagesOnTimeout is the v2-family mirror: a
// GetApis timeout behind a NextToken keeps the first-page APIs plus the
// timeout warning.
func TestListV2APIsKeepsEarlierPagesOnTimeout(t *testing.T) {
	v2 := &fakeV2API{
		v2APIPages: []*awsapigatewayv2.GetApisOutput{{
			Items: []awsapigatewayv2types.Api{{
				ApiId:        aws.String("v2-1"),
				Name:         aws.String("orders-http"),
				ProtocolType: awsapigatewayv2types.ProtocolTypeHttp,
			}},
			NextToken: aws.String("token-two"),
		}},
		v2APIErrors: []error{nil, dialTimeoutError()},
	}
	adapter := newTimeoutTestClient(emptyTimeoutREST(), v2)
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (timeout must degrade with warning)", err)
	}
	if got, want := len(snapshot.V2APIs), 1; got != want {
		t.Fatalf("len(V2APIs) = %d, want %d (page-one API survives the page-two timeout)", got, want)
	}
	singleTimeoutWarning(t, snapshot, "GetApis")
}

// TestListRESTIntegrationsKeepsEarlierPagesOnTimeout proves that a
// GetResources timeout behind a Position keeps the page-one integrations
// plus the timeout warning instead of an empty slice with a lone warning.
func TestListRESTIntegrationsKeepsEarlierPagesOnTimeout(t *testing.T) {
	rest := &fakeRESTAPI{
		restAPIPages: []*awsapigateway.GetRestApisOutput{{
			Items: []awsapigatewaytypes.RestApi{{Id: aws.String("rest-1"), Name: aws.String("orders-rest")}},
		}},
		restStagePages: []*awsapigateway.GetStagesOutput{{}},
		restResourcePages: []*awsapigateway.GetResourcesOutput{{
			Items: []awsapigatewaytypes.Resource{{
				Id:   aws.String("res-1"),
				Path: aws.String("/orders"),
				ResourceMethods: map[string]awsapigatewaytypes.Method{"GET": {
					MethodIntegration: &awsapigatewaytypes.Integration{
						Type: awsapigatewaytypes.IntegrationTypeAwsProxy,
						Uri:  aws.String("arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/orders/invocations"),
					},
				}},
			}},
			Position: aws.String("page-two"),
		}},
		restResourceErrors: []error{nil, dialTimeoutError()},
	}
	adapter := newTimeoutTestClient(rest, emptyTimeoutV2())
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (timeout must degrade with warning)", err)
	}
	if got, want := len(snapshot.RESTAPIs), 1; got != want {
		t.Fatalf("len(RESTAPIs) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.RESTAPIs[0].Integrations), 1; got != want {
		t.Fatalf("len(REST integrations) = %d, want %d (page-one integration survives the page-two timeout)", got, want)
	}
	singleTimeoutWarning(t, snapshot, "GetResources")
}

// TestListRESTMappingsKeepsEarlierPagesOnTimeout is the domain-mapping
// mirror: a GetBasePathMappings timeout behind a Position keeps the
// page-one mappings plus the timeout warning.
func TestListRESTMappingsKeepsEarlierPagesOnTimeout(t *testing.T) {
	rest := &fakeRESTAPI{
		restDomainPages: []*awsapigateway.GetDomainNamesOutput{{
			Items: []awsapigatewaytypes.DomainName{{DomainName: aws.String("api.example.com")}},
		}},
		restMappingPages: []*awsapigateway.GetBasePathMappingsOutput{{
			Items: []awsapigatewaytypes.BasePathMapping{{
				BasePath:  aws.String("v1"),
				RestApiId: aws.String("rest-1"),
				Stage:     aws.String("prod"),
			}},
			Position: aws.String("page-two"),
		}},
		restMappingErrors: []error{nil, dialTimeoutError()},
	}
	adapter := newTimeoutTestClient(rest, emptyTimeoutV2())
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (timeout must degrade with warning)", err)
	}
	if got, want := len(snapshot.Domains), 1; got != want {
		t.Fatalf("len(Domains) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Domains[0].Mappings), 1; got != want {
		t.Fatalf("len(Domain mappings) = %d, want %d (page-one mapping survives the page-two timeout)", got, want)
	}
	singleTimeoutWarning(t, snapshot, "GetBasePathMappings")
}

// TestListV2StagesKeepsEarlierPagesOnTimeout proves that a v2 GetStages
// timeout behind a NextToken keeps the page-one stages plus the timeout
// warning.
func TestListV2StagesKeepsEarlierPagesOnTimeout(t *testing.T) {
	v2 := &fakeV2API{
		v2APIPages: []*awsapigatewayv2.GetApisOutput{{
			Items: []awsapigatewayv2types.Api{{
				ApiId:        aws.String("v2-1"),
				Name:         aws.String("orders-http"),
				ProtocolType: awsapigatewayv2types.ProtocolTypeHttp,
			}},
		}},
		v2StagePages: []*awsapigatewayv2.GetStagesOutput{{
			Items:     []awsapigatewayv2types.Stage{{StageName: aws.String("prod")}},
			NextToken: aws.String("token-two"),
		}},
		v2StageErrors: []error{nil, dialTimeoutError()},
	}
	adapter := newTimeoutTestClient(emptyTimeoutREST(), v2)
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (timeout must degrade with warning)", err)
	}
	if got, want := len(snapshot.V2APIs), 1; got != want {
		t.Fatalf("len(V2APIs) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.V2APIs[0].Stages), 1; got != want {
		t.Fatalf("len(V2 stages) = %d, want %d (page-one stage survives the page-two timeout)", got, want)
	}
	singleTimeoutWarning(t, snapshot, "GetStages")
}

// TestListV2IntegrationsKeepsEarlierPagesOnTimeout is the v2-integration
// mirror: a GetIntegrations timeout behind a NextToken keeps the page-one
// integrations plus the timeout warning.
func TestListV2IntegrationsKeepsEarlierPagesOnTimeout(t *testing.T) {
	v2 := &fakeV2API{
		v2APIPages: []*awsapigatewayv2.GetApisOutput{{
			Items: []awsapigatewayv2types.Api{{
				ApiId:        aws.String("v2-1"),
				Name:         aws.String("orders-http"),
				ProtocolType: awsapigatewayv2types.ProtocolTypeHttp,
			}},
		}},
		v2StagePages: []*awsapigatewayv2.GetStagesOutput{{}},
		v2IntegrationPages: []*awsapigatewayv2.GetIntegrationsOutput{{
			Items: []awsapigatewayv2types.Integration{{
				IntegrationId:   aws.String("int-1"),
				IntegrationType: awsapigatewayv2types.IntegrationTypeAwsProxy,
			}},
			NextToken: aws.String("token-two"),
		}},
		v2IntegrationErrors: []error{nil, dialTimeoutError()},
	}
	adapter := newTimeoutTestClient(emptyTimeoutREST(), v2)
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (timeout must degrade with warning)", err)
	}
	if got, want := len(snapshot.V2APIs), 1; got != want {
		t.Fatalf("len(V2APIs) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.V2APIs[0].Integrations), 1; got != want {
		t.Fatalf("len(V2 integrations) = %d, want %d (page-one integration survives the page-two timeout)", got, want)
	}
	singleTimeoutWarning(t, snapshot, "GetIntegrations")
}

// TestListV2MappingsKeepsEarlierPagesOnTimeout is the v2-mapping mirror: a
// GetApiMappings timeout behind a NextToken keeps the page-one mappings
// plus the timeout warning.
func TestListV2MappingsKeepsEarlierPagesOnTimeout(t *testing.T) {
	v2 := &fakeV2API{
		v2DomainPages: []*awsapigatewayv2.GetDomainNamesOutput{{
			Items: []awsapigatewayv2types.DomainName{{DomainName: aws.String("http.example.com")}},
		}},
		v2MappingPages: []*awsapigatewayv2.GetApiMappingsOutput{{
			Items: []awsapigatewayv2types.ApiMapping{{
				ApiMappingId:  aws.String("map-1"),
				ApiMappingKey: aws.String("orders"),
				ApiId:         aws.String("v2-1"),
				Stage:         aws.String("$default"),
			}},
			NextToken: aws.String("token-two"),
		}},
		v2MappingErrors: []error{nil, dialTimeoutError()},
	}
	adapter := newTimeoutTestClient(emptyTimeoutREST(), v2)
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (timeout must degrade with warning)", err)
	}
	if got, want := len(snapshot.Domains), 1; got != want {
		t.Fatalf("len(Domains) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Domains[0].Mappings), 1; got != want {
		t.Fatalf("len(Domain mappings) = %d, want %d (page-one mapping survives the page-two timeout)", got, want)
	}
	singleTimeoutWarning(t, snapshot, "GetApiMappings")
}

// TestSnapshotPropagatesCallerCancellationTerminal proves that a transport
// timeout observed under an already-done scan context stays terminal: only
// SDK-internal retry-exhausted timeouts degrade with a warning.
func TestSnapshotPropagatesCallerCancellationTerminal(t *testing.T) {
	cases := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{name: "cancelled", ctx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			return ctx, cancel
		}},
		{name: "deadline exceeded", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			cancel()
			adapter := newTimeoutTestClient(
				&fakeRESTAPI{restAPIErrors: []error{dialTimeoutError()}},
				emptyTimeoutV2(),
			)
			snapshot, err := adapter.Snapshot(ctx)
			if err == nil {
				t.Fatal("Snapshot() error = nil, want terminal propagation when the caller context is done")
			}
			if got := len(snapshot.Warnings); got != 0 {
				t.Fatalf("len(Warnings) = %d, want 0 when caller cancellation stays terminal", got)
			}
		})
	}
}
