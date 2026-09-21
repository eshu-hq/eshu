// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigateway "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotRecordsWarningWhenRESTResourcesThrottle(t *testing.T) {
	adapter := newThrottleTestClient(&fakeRESTAPI{
		restAPIPages: []*awsapigateway.GetRestApisOutput{{
			Items: []awsapigatewaytypes.RestApi{{
				Id:   aws.String("rest-1"),
				Name: aws.String("orders-rest"),
			}},
		}},
		restStagePages: []*awsapigateway.GetStagesOutput{{}},
		restResourceErrors: []error{&smithy.GenericAPIError{
			Code:    "TooManyRequestsException",
			Message: "Too Many Requests",
		}},
		restDomainPages: []*awsapigateway.GetDomainNamesOutput{{}},
	})
	recorder := awscloud.NewAPICallStatsRecorder(adapter.boundary)
	ctx := awscloud.ContextWithAPICallRecorder(context.Background(), recorder)

	snapshot, err := adapter.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.RESTAPIs), 1; got != want {
		t.Fatalf("len(RESTAPIs) = %d, want %d", got, want)
	}
	if got := len(snapshot.RESTAPIs[0].Integrations); got != 0 {
		t.Fatalf("len(REST integrations) = %d, want 0 after throttled GetResources", got)
	}
	if got, want := len(snapshot.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	warning := snapshot.Warnings[0]
	if warning.WarningKind != awscloud.WarningThrottleSustained {
		t.Fatalf("warning kind = %q, want %q", warning.WarningKind, awscloud.WarningThrottleSustained)
	}
	if warning.ErrorClass != "throttled" {
		t.Fatalf("warning error class = %q, want throttled", warning.ErrorClass)
	}
	stats := recorder.Snapshot()
	if got, want := stats.ThrottleCount, 1; got != want {
		t.Fatalf("ThrottleCount = %d, want %d", got, want)
	}
}

func TestClientSnapshotDiscardsPartialRESTIntegrationsWhenLaterPageThrottles(t *testing.T) {
	adapter := newThrottleTestClient(&fakeRESTAPI{
		restAPIPages: []*awsapigateway.GetRestApisOutput{{
			Items: []awsapigatewaytypes.RestApi{{
				Id:   aws.String("rest-1"),
				Name: aws.String("orders-rest"),
			}},
		}},
		restStagePages: []*awsapigateway.GetStagesOutput{{}},
		restResourcePages: []*awsapigateway.GetResourcesOutput{{
			Items: []awsapigatewaytypes.Resource{{
				Id:   aws.String("res-1"),
				Path: aws.String("/orders"),
				ResourceMethods: map[string]awsapigatewaytypes.Method{"POST": {
					MethodIntegration: &awsapigatewaytypes.Integration{
						Type: awsapigatewaytypes.IntegrationTypeAwsProxy,
						Uri:  aws.String("arn:aws:lambda:us-east-1:123456789012:function:orders"),
					},
				}},
			}},
			Position: aws.String("next-page"),
		}},
		restResourceErrors: []error{nil, &smithy.GenericAPIError{
			Code:    "TooManyRequestsException",
			Message: "Too Many Requests",
		}},
		restDomainPages: []*awsapigateway.GetDomainNamesOutput{{}},
	})

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got := len(snapshot.RESTAPIs[0].Integrations); got != 0 {
		t.Fatalf("len(REST integrations) = %d, want 0 so partial relationships are not emitted", got)
	}
	if got, want := len(snapshot.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
}

func TestClientSnapshotDeduplicatesRESTResourceThrottleWarnings(t *testing.T) {
	adapter := newThrottleTestClient(&fakeRESTAPI{
		restAPIPages: []*awsapigateway.GetRestApisOutput{{
			Items: []awsapigatewaytypes.RestApi{{
				Id:   aws.String("rest-1"),
				Name: aws.String("orders-rest"),
			}, {
				Id:   aws.String("rest-2"),
				Name: aws.String("billing-rest"),
			}},
		}},
		restStagePages: []*awsapigateway.GetStagesOutput{{}, {}},
		restResourceErrors: []error{
			&smithy.GenericAPIError{Code: "TooManyRequestsException", Message: "Too Many Requests"},
			&smithy.GenericAPIError{Code: "TooManyRequestsException", Message: "Too Many Requests"},
		},
		restDomainPages: []*awsapigateway.GetDomainNamesOutput{{}},
	})

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.RESTAPIs), 2; got != want {
		t.Fatalf("len(RESTAPIs) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
}

func newThrottleTestClient(rest *fakeRESTAPI) *Client {
	return &Client{
		rest: rest,
		v2:   &fakeV2API{v2APIPages: []*awsapigatewayv2.GetApisOutput{{}}, v2DomainPages: []*awsapigatewayv2.GetDomainNamesOutput{{}}},
		boundary: awscloud.Boundary{
			AccountID:           "123456789012",
			Region:              "us-east-1",
			ServiceKind:         awscloud.ServiceAPIGateway,
			ScopeID:             "aws:123456789012:us-east-1:apigateway",
			GenerationID:        "generation-1",
			CollectorInstanceID: "collector-1",
			FencingToken:        7,
		},
	}
}
