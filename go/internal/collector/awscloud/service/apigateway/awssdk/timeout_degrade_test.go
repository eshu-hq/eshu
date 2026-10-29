// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigateway "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/service/apigateway"
)

// dialTimeoutError mirrors the QA outage behind #9535: the SDK exhausts its
// attempts against an unreachable VPC interface endpoint and surfaces a dial
// i/o timeout through the transport chain.
func dialTimeoutError() error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
}

// retryExhaustedTimeoutMessage mirrors the same outage when it arrives as a
// flattened SDK message instead of a wrapped transport error.
func retryExhaustedTimeoutMessage() error {
	return errors.New(`operation error ApiGateway: GetRestApis, exceeded maximum number of attempts, 3, ` +
		`request send failed, Post "https://apigateway.us-east-1.amazonaws.com/restapis": ` +
		`dial tcp 10.247.1.2:443: i/o timeout`)
}

func accessDeniedError() error {
	return &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "User is not authorized to perform this action"}
}

func newTimeoutTestClient(rest *fakeRESTAPI, v2 *fakeV2API) *Client {
	return &Client{
		rest: rest,
		v2:   v2,
		boundary: awscloud.Boundary{
			AccountID:           "123456789012",
			Region:              "us-east-1",
			ServiceKind:         catalog.ServiceAPIGateway,
			ScopeID:             "aws:123456789012:us-east-1:apigateway",
			GenerationID:        "generation-1",
			CollectorInstanceID: "collector-1",
			FencingToken:        7,
		},
	}
}

func emptyTimeoutV2() *fakeV2API {
	return &fakeV2API{
		v2APIPages:    []*awsapigatewayv2.GetApisOutput{{}},
		v2DomainPages: []*awsapigatewayv2.GetDomainNamesOutput{{}},
	}
}

func emptyTimeoutREST() *fakeRESTAPI {
	return &fakeRESTAPI{
		restAPIPages:    []*awsapigateway.GetRestApisOutput{{}},
		restDomainPages: []*awsapigateway.GetDomainNamesOutput{{}},
	}
}

func singleTimeoutWarning(t *testing.T, snapshot apigatewayservice.Snapshot, operation string) awscloud.WarningObservation {
	t.Helper()
	if got, want := len(snapshot.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	warning := snapshot.Warnings[0]
	if warning.WarningKind != awscloud.WarningNetworkTimeout {
		t.Fatalf("warning kind = %q, want %q", warning.WarningKind, awscloud.WarningNetworkTimeout)
	}
	if warning.ErrorClass != "network_timeout" {
		t.Fatalf("warning error class = %q, want network_timeout", warning.ErrorClass)
	}
	if got, _ := warning.Attributes["operation"].(string); got != operation {
		t.Fatalf("warning operation = %q, want %q", got, operation)
	}
	if strings.Contains(warning.Message, "10.247") {
		t.Fatalf("warning message leaks endpoint address: %q", warning.Message)
	}
	return warning
}

func TestClientSnapshotDegradesWithWarningOnNetworkTimeout(t *testing.T) {
	restAPIPage := []*awsapigateway.GetRestApisOutput{{
		Items: []awsapigatewaytypes.RestApi{{Id: aws.String("rest-1"), Name: aws.String("orders-rest")}},
	}}
	restDomainPage := []*awsapigateway.GetDomainNamesOutput{{
		Items: []awsapigatewaytypes.DomainName{{DomainName: aws.String("api.example.com")}},
	}}
	v2APIPage := []*awsapigatewayv2.GetApisOutput{{
		Items: []awsapigatewayv2types.Api{{
			ApiId:        aws.String("v2-1"),
			Name:         aws.String("orders-http"),
			ProtocolType: awsapigatewayv2types.ProtocolTypeHttp,
		}},
	}}
	v2DomainPage := []*awsapigatewayv2.GetDomainNamesOutput{{
		Items: []awsapigatewayv2types.DomainName{{DomainName: aws.String("http.example.com")}},
	}}

	cases := []struct {
		name          string
		rest          *fakeRESTAPI
		v2            *fakeV2API
		operation     string
		checkSnapshot func(t *testing.T, snapshot apigatewayservice.Snapshot)
	}{
		{
			name:      "rest apis",
			rest:      &fakeRESTAPI{restAPIErrors: []error{dialTimeoutError()}},
			v2:        emptyTimeoutV2(),
			operation: "GetRestApis",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.RESTAPIs); got != 0 {
					t.Fatalf("len(RESTAPIs) = %d, want 0 on GetRestApis timeout", got)
				}
			},
		},
		{
			name: "rest stages",
			rest: &fakeRESTAPI{
				restAPIPages:    restAPIPage,
				restStageErrors: []error{dialTimeoutError()},
			},
			v2:        emptyTimeoutV2(),
			operation: "GetStages",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.RESTAPIs); got != 1 {
					t.Fatalf("len(RESTAPIs) = %d, want 1 to keep the listed API", got)
				}
				if got := len(snapshot.RESTAPIs[0].Stages); got != 0 {
					t.Fatalf("len(REST stages) = %d, want 0 on GetStages timeout", got)
				}
			},
		},
		{
			name: "rest resources",
			rest: &fakeRESTAPI{
				restAPIPages:       restAPIPage,
				restStagePages:     []*awsapigateway.GetStagesOutput{{}},
				restResourceErrors: []error{dialTimeoutError()},
			},
			v2:        emptyTimeoutV2(),
			operation: "GetResources",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.RESTAPIs[0].Integrations); got != 0 {
					t.Fatalf("len(REST integrations) = %d, want 0 on GetResources timeout", got)
				}
			},
		},
		{
			name:      "rest domains",
			rest:      &fakeRESTAPI{restDomainErrors: []error{dialTimeoutError()}},
			v2:        emptyTimeoutV2(),
			operation: "GetDomainNames",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.Domains); got != 0 {
					t.Fatalf("len(Domains) = %d, want 0 on GetDomainNames timeout", got)
				}
			},
		},
		{
			name: "rest mappings",
			rest: &fakeRESTAPI{
				restDomainPages:   restDomainPage,
				restMappingErrors: []error{dialTimeoutError()},
			},
			v2:        emptyTimeoutV2(),
			operation: "GetBasePathMappings",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.Domains); got != 1 {
					t.Fatalf("len(Domains) = %d, want 1 to keep the listed domain", got)
				}
				if got := len(snapshot.Domains[0].Mappings); got != 0 {
					t.Fatalf("len(Domain mappings) = %d, want 0 on GetBasePathMappings timeout", got)
				}
			},
		},
		{
			name:      "v2 apis",
			rest:      emptyTimeoutREST(),
			v2:        &fakeV2API{v2APIErrors: []error{dialTimeoutError()}},
			operation: "GetApis",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.V2APIs); got != 0 {
					t.Fatalf("len(V2APIs) = %d, want 0 on GetApis timeout", got)
				}
			},
		},
		{
			name: "v2 stages",
			rest: emptyTimeoutREST(),
			v2: &fakeV2API{
				v2APIPages:    v2APIPage,
				v2StageErrors: []error{dialTimeoutError()},
			},
			operation: "GetStages",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.V2APIs); got != 1 {
					t.Fatalf("len(V2APIs) = %d, want 1 to keep the listed API", got)
				}
				if got := len(snapshot.V2APIs[0].Stages); got != 0 {
					t.Fatalf("len(V2 stages) = %d, want 0 on GetStages timeout", got)
				}
			},
		},
		{
			name: "v2 integrations",
			rest: emptyTimeoutREST(),
			v2: &fakeV2API{
				v2APIPages:          v2APIPage,
				v2StagePages:        []*awsapigatewayv2.GetStagesOutput{{}},
				v2IntegrationErrors: []error{dialTimeoutError()},
			},
			operation: "GetIntegrations",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.V2APIs[0].Integrations); got != 0 {
					t.Fatalf("len(V2 integrations) = %d, want 0 on GetIntegrations timeout", got)
				}
			},
		},
		{
			name:      "v2 domains",
			rest:      emptyTimeoutREST(),
			v2:        &fakeV2API{v2DomainErrors: []error{dialTimeoutError()}},
			operation: "GetDomainNames",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.Domains); got != 0 {
					t.Fatalf("len(Domains) = %d, want 0 on GetDomainNames timeout", got)
				}
			},
		},
		{
			name: "v2 mappings",
			rest: emptyTimeoutREST(),
			v2: &fakeV2API{
				v2DomainPages:   v2DomainPage,
				v2MappingErrors: []error{dialTimeoutError()},
			},
			operation: "GetApiMappings",
			checkSnapshot: func(t *testing.T, snapshot apigatewayservice.Snapshot) {
				t.Helper()
				if got := len(snapshot.Domains); got != 1 {
					t.Fatalf("len(Domains) = %d, want 1 to keep the listed domain", got)
				}
				if got := len(snapshot.Domains[0].Mappings); got != 0 {
					t.Fatalf("len(Domain mappings) = %d, want 0 on GetApiMappings timeout", got)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := newTimeoutTestClient(tc.rest, tc.v2)
			snapshot, err := adapter.Snapshot(context.Background())
			if err != nil {
				t.Fatalf("Snapshot() error = %v, want nil (timeout must degrade with warning)", err)
			}
			singleTimeoutWarning(t, snapshot, tc.operation)
			tc.checkSnapshot(t, snapshot)
		})
	}
}

func TestClientSnapshotDegradesOnEveryTimeoutShape(t *testing.T) {
	shapes := []struct {
		name string
		err  error
	}{
		{name: "wrapped transport timeout", err: dialTimeoutError()},
		{name: "retry-exhausted message", err: retryExhaustedTimeoutMessage()},
		{name: "context deadline", err: context.DeadlineExceeded},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			adapter := newTimeoutTestClient(
				&fakeRESTAPI{restAPIErrors: []error{shape.err}},
				emptyTimeoutV2(),
			)
			snapshot, err := adapter.Snapshot(context.Background())
			if err != nil {
				t.Fatalf("Snapshot() error = %v, want nil", err)
			}
			if got := len(snapshot.RESTAPIs); got != 0 {
				t.Fatalf("len(RESTAPIs) = %d, want 0", got)
			}
			singleTimeoutWarning(t, snapshot, "GetRestApis")
		})
	}
}

func TestClientSnapshotStaysFatalOnNonTimeoutErrors(t *testing.T) {
	cases := []struct {
		name      string
		rest      *fakeRESTAPI
		v2        *fakeV2API
		operation string
	}{
		{
			name:      "access denied on rest apis",
			rest:      &fakeRESTAPI{restAPIErrors: []error{accessDeniedError()}},
			v2:        emptyTimeoutV2(),
			operation: "GetRestApis",
		},
		{
			name: "contract error on rest apis",
			rest: &fakeRESTAPI{restAPIErrors: []error{&smithy.GenericAPIError{
				Code:    "ValidationException",
				Message: "Invalid limit",
			}}},
			v2:        emptyTimeoutV2(),
			operation: "GetRestApis",
		},
		{
			name: "refused connection on rest apis",
			rest: &fakeRESTAPI{restAPIErrors: []error{errors.New(
				"dial tcp 10.0.0.1:443: connect: connection refused")}},
			v2:        emptyTimeoutV2(),
			operation: "GetRestApis",
		},
		{
			name:      "access denied on v2 apis",
			rest:      emptyTimeoutREST(),
			v2:        &fakeV2API{v2APIErrors: []error{accessDeniedError()}},
			operation: "GetApis",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := newTimeoutTestClient(tc.rest, tc.v2)
			snapshot, err := adapter.Snapshot(context.Background())
			if err == nil {
				t.Fatalf("Snapshot() error = nil on %s, want a terminal error", tc.operation)
			}
			if got := len(snapshot.Warnings); got != 0 {
				t.Fatalf("len(Warnings) = %d, want 0 when %s stays fatal", got, tc.operation)
			}
		})
	}
}
