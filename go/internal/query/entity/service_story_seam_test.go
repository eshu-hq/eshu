// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestBuildServiceStoryEnvelopeRequiresServiceName(t *testing.T) {
	t.Parallel()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	data, truth, status, errEnv := handler.BuildServiceStoryEnvelope(context.Background(), service.WorkloadSelector{}, "service_story")
	if data != nil || truth != nil {
		t.Fatalf("missing service name should yield no data/truth, got data=%v truth=%v", data, truth)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if errEnv == nil || errEnv.Code != querycontract.ErrorCodeInvalidArgument {
		t.Fatalf("errEnv = %#v, want invalid_argument", errEnv)
	}
}

func TestBuildServiceStoryEnvelopeMissingServiceReturnsNotFound(t *testing.T) {
	t.Parallel()
	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunSingleByMatch: map[string]map[string]any{},
			RunByMatch:       map[string][]map[string]any{},
		},
		Profile: querycontract.ProfileProduction,
	}
	data, truth, status, errEnv := handler.BuildServiceStoryEnvelope(context.Background(), service.WorkloadSelector{ServiceName: "missing"}, "service_story")
	if data != nil || truth != nil {
		t.Fatalf("missing service should yield no data/truth")
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; errEnv=%#v", status, errEnv)
	}
	if errEnv == nil || errEnv.Code != querycontract.ErrorCodeNotFound {
		t.Fatalf("errEnv = %#v, want not_found", errEnv)
	}
}

func TestBuildServiceStoryEnvelopeUnsupportedCapability(t *testing.T) {
	t.Parallel()
	// Local lightweight profile does not support the platform context capability.
	handler := &Handler{Profile: querycontract.ProfileLocalLightweight}
	_, _, status, errEnv := handler.BuildServiceStoryEnvelope(context.Background(), service.WorkloadSelector{ServiceName: "checkout"}, "service_story")
	if status != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; errEnv=%#v", status, errEnv)
	}
	if errEnv == nil || errEnv.Code != querycontract.ErrorCodeUnsupportedCapability || errEnv.Profiles == nil {
		t.Fatalf("errEnv = %#v, want unsupported_capability with profiles", errEnv)
	}
}

// TestBuildServiceStoryEnvelopeMapsGraphReadAvailabilityErrors proves the
// envelope-returning seam maps bounded graph-read sentinels onto the same
// 503/504 contract the HTTP handlers use. BuildServiceStoryEnvelope returns an
// envelope instead of writing the response, so it cannot call
// WriteGraphReadError; without the graphReadErrorEnvelope guard these
// sentinels collapse into a generic 500.
func TestBuildServiceStoryEnvelopeMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   querycontract.ErrorCode
	}{
		{name: "unavailable", err: fmt.Errorf("private address: %w", querycontract.ErrGraphUnavailable), wantStatus: http.StatusServiceUnavailable, wantCode: querycontract.ErrorCodeBackendUnavailable},
		{name: "deadline", err: fmt.Errorf("private query: %w", querycontract.ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, wantCode: querycontract.ErrorCodeBackendTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &Handler{
				Neo4j: graph.FakeGraphReader{
					RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
						return nil, test.err
					},
					RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
						return nil, test.err
					},
				},
				Profile: querycontract.ProfileProduction,
			}

			data, truth, status, errEnv := handler.BuildServiceStoryEnvelope(
				context.Background(), service.WorkloadSelector{ServiceName: "orders-api"}, "service_story",
			)

			if data != nil || truth != nil {
				t.Fatalf("graph-read failure should yield no data/truth, got data=%v truth=%v", data, truth)
			}
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d (errEnv=%#v)", status, test.wantStatus, errEnv)
			}
			if errEnv == nil || errEnv.Code != test.wantCode {
				t.Fatalf("errEnv = %#v, want code %q", errEnv, test.wantCode)
			}
			if strings.Contains(errEnv.Message, "private") {
				t.Fatalf("envelope leaked private cause: %s", errEnv.Message)
			}
		})
	}
}

// TestServiceStoryResolutionNotFoundAnswersSelectorText is the #7674
// regression for the service story not-found envelope: it echoed err.Error(),
// so a wrapper around the selector's NotFoundError would reach the client. It
// must answer only the typed error's own text, which quotes the caller's
// selector.
func TestServiceStoryResolutionNotFoundAnswersSelectorText(t *testing.T) {
	t.Parallel()

	notFound := selector.NotFoundError{Selector: "payments"}
	status, errEnv := serviceStoryResolutionError(context.Background(), fmt.Errorf("private wrapper %s: %w", failure.Canary, notFound))
	if status != http.StatusNotFound || errEnv == nil {
		t.Fatalf("status = %d, envelope = %#v; want 404 with an envelope", status, errEnv)
	}
	if errEnv.Message != notFound.Error() {
		t.Fatalf("message = %q, want the selector's own %q", errEnv.Message, notFound.Error())
	}
}
