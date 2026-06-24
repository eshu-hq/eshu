// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
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
