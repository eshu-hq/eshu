// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestDriftPacketAnswersFixedServerFailures is the #7674 slice 9 regression
// for the drift packet readback that opens a handler span: it answered 500
// with the store error text (err.Error()), answered a stale reader with that
// 500 instead of the retryable 503, and answered a client cancel as a server
// fault. It split out of TestEvidenceRoutesAnswerFixedServerFailures in the
// #6642 evidence-leaf move, which carried the evidence cases to
// internal/query/evidence; the drift packet belongs to another family and
// stays here. Not parallel: it swaps queryHandlerTracer.
func TestDriftPacketAnswersFixedServerFailures(t *testing.T) {
	route := statusFailureRoute{
		route: failure.Route{
			Name: "drift packet", Path: "/api/v0/investigations/drift/packet?scope_id=aws-account-1",
			Message: driftPacketReadFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &CloudRuntimeDriftHandler{Store: fakeMultiCloudRuntimeDriftStore{listErr: err}}
			},
		},
		capability: cloudRuntimeDriftReadbackCapability,
	}
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer, Routes: []failure.Route{route.route}}
	failure.Run(t, suite)
	assertFenceCapability(t, suite, route.route, route.capability)
}
