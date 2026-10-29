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
