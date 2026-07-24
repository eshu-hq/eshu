// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestBuildServiceStoryEnvelopeRequiresServiceName(t *testing.T) {
	t.Parallel()
	handler := &EntityHandler{Profile: ProfileProduction}
	data, truth, status, errEnv := handler.BuildServiceStoryEnvelope(context.Background(), ServiceWorkloadSelector{}, "service_story")
	if data != nil || truth != nil {
		t.Fatalf("missing service name should yield no data/truth, got data=%v truth=%v", data, truth)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if errEnv == nil || errEnv.Code != ErrorCodeInvalidArgument {
		t.Fatalf("errEnv = %#v, want invalid_argument", errEnv)
	}
}

func TestBuildServiceStoryEnvelopeMissingServiceReturnsNotFound(t *testing.T) {
	t.Parallel()
	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{},
			runByMatch:       map[string][]map[string]any{},
		},
		Profile: ProfileProduction,
	}
	data, truth, status, errEnv := handler.BuildServiceStoryEnvelope(context.Background(), ServiceWorkloadSelector{ServiceName: "missing"}, "service_story")
	if data != nil || truth != nil {
		t.Fatalf("missing service should yield no data/truth")
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; errEnv=%#v", status, errEnv)
	}
	if errEnv == nil || errEnv.Code != ErrorCodeNotFound {
		t.Fatalf("errEnv = %#v, want not_found", errEnv)
	}
}

func TestBuildServiceStoryEnvelopeUnsupportedCapability(t *testing.T) {
	t.Parallel()
	// Local lightweight profile does not support the platform context capability.
	handler := &EntityHandler{Profile: ProfileLocalLightweight}
	_, _, status, errEnv := handler.BuildServiceStoryEnvelope(context.Background(), ServiceWorkloadSelector{ServiceName: "checkout"}, "service_story")
	if status != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; errEnv=%#v", status, errEnv)
	}
	if errEnv == nil || errEnv.Code != ErrorCodeUnsupportedCapability || errEnv.Profiles == nil {
		t.Fatalf("errEnv = %#v, want unsupported_capability with profiles", errEnv)
	}
}

// TestBuildServiceStoryEnvelopeMapsGraphReadAvailabilityErrors proves the
// envelope-returning seam maps bounded graph-read sentinels onto the same
// 503/504 contract the HTTP handlers use. BuildServiceStoryEnvelope returns an
// envelope instead of writing the response, so it cannot call
// WriteGraphReadError; without the graphReadErrorEnvelope guard these
// sentinels collapse into a generic 500 that also leaks the private cause
// through serviceStoryInternalError's "%v" formatting.
func TestBuildServiceStoryEnvelopeMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   ErrorCode
	}{
		{name: "unavailable", err: fmt.Errorf("private address: %w", ErrGraphUnavailable), wantStatus: http.StatusServiceUnavailable, wantCode: ErrorCodeBackendUnavailable},
		{name: "deadline", err: fmt.Errorf("private query: %w", ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, wantCode: ErrorCodeBackendTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &EntityHandler{
				Neo4j: fakeGraphReader{
					run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
						return nil, test.err
					},
					runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
						return nil, test.err
					},
				},
				Profile: ProfileProduction,
			}

			data, truth, status, errEnv := handler.BuildServiceStoryEnvelope(
				context.Background(), ServiceWorkloadSelector{ServiceName: "orders-api"}, "service_story",
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
