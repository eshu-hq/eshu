// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// failingCloudInventoryStore is a ContentStore whose canonical identity read
// fails with err. Only the readback methods are implemented; the embedded nil
// ContentStore panics if the handler reaches any other read.
type failingCloudInventoryStore struct {
	ContentStore
	err error
}

func (s failingCloudInventoryStore) CloudInventoryIdentities(context.Context, cloudInventoryFilter) (cloudInventoryListReadModel, error) {
	return cloudInventoryListReadModel{}, s.err
}

func (s failingCloudInventoryStore) CloudInventoryPreRolloutEvidenceExists(context.Context, cloudInventoryFilter) (bool, error) {
	return false, nil
}

// cloudInventoryFailureRoute is the one failure site of GET
// /api/v0/cloud/inventory: the canonical identity read.
func cloudInventoryFailureRoute() failure.Route {
	return failure.Route{
		Name: "cloud inventory readback", Path: "/api/v0/cloud/inventory?provider=aws",
		Message: cloudInventoryReadbackFailedMessage,
		Handler: func(err error) failure.Mounter {
			return &CloudInventoryHandler{Content: failingCloudInventoryStore{err: err}, Profile: ProfileProduction}
		},
	}
}

// TestCloudInventoryReadbackAnswersFixedServerFailures is the #7674
// regression for GET /api/v0/cloud/inventory. The route answered every read
// failure with a direct 500: a stale or timed-out PostgreSQL reader got that
// 500 instead of the retryable 503, and a client cancel read as a server fault
// with no span record. The canonical identity read goes through the guarded
// reader, so the fence answers 503 with Retry-After, and a client cancel
// answers 499 with the span left Unset.
func TestCloudInventoryReadbackAnswersFixedServerFailures(t *testing.T) {
	route := cloudInventoryFailureRoute()
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer, Routes: []failure.Route{route}}
	failure.Run(t, suite)
	assertFenceCapability(t, suite, route, cloudInventoryReadbackCapability)
}

// TestCloudInventoryReadbackFailureKeepsContractEnvelope pins that the 500
// keeps the route's contract envelope for an envelope client: code
// internal_error, the readback capability, and the current and required
// profiles, as the direct WriteContractError answered before.
func TestCloudInventoryReadbackFailureKeepsContractEnvelope(t *testing.T) {
	route := cloudInventoryFailureRoute()
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer, Routes: []failure.Route{route}}
	rec, _ := failure.Serve(suite, route, route.Handler(fmt.Errorf("%s: boom", failure.Canary)), false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error *ErrorEnvelope `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == nil {
		t.Fatalf("decode envelope: %v; body = %s", err, rec.Body.String())
	}
	got := body.Error
	if got.Code != ErrorCodeInternalError || got.Message != cloudInventoryReadbackFailedMessage ||
		got.Capability != cloudInventoryReadbackCapability {
		t.Fatalf("envelope = %+v, want internal_error %q %q", got, cloudInventoryReadbackFailedMessage, cloudInventoryReadbackCapability)
	}
	if got.Profiles == nil || got.Profiles.Current != ProfileProduction ||
		got.Profiles.Required != querycontract.RequiredProfile(cloudInventoryReadbackCapability) {
		t.Fatalf("envelope profiles = %+v, want current %q and the capability's required profile", got.Profiles, ProfileProduction)
	}
}
