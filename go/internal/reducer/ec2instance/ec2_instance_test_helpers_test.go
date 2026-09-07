// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2instance

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Local copies of the reducer-root test helpers this family's tests used
// before the move (issue #6061). Go test files cannot share unexported
// symbols across a package boundary, so they are duplicated here verbatim
// rather than exported from the root for test-only use.

// stubFactLoader replays a fixed envelope batch and counts loads.
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

// readyLookup returns a ReadinessLookup that always answers (ready, found).
func readyLookup(ready, found bool) gpphase.ReadinessLookup {
	return func(_ gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		return ready, found
	}
}

// recordingGraphProjectionPhasePublisher captures the readiness phases the
// node handler publishes so tests can assert the canonical-nodes-committed
// publication without a durable backend.
type recordingGraphProjectionPhasePublisher struct {
	calls [][]gpphase.PhaseState
	err   error
}

func (r *recordingGraphProjectionPhasePublisher) PublishGraphProjectionPhases(_ context.Context, rows []gpphase.PhaseState) error {
	cloned := make([]gpphase.PhaseState, len(rows))
	copy(cloned, rows)
	r.calls = append(r.calls, cloned)
	return r.err
}

// recordingEC2InstanceNodeWriter captures the rows handed to the node writer so
// tests can assert on the exact materialization request.
type recordingEC2InstanceNodeWriter struct {
	calls          int
	rows           []map[string]any
	evidenceSource string
	err            error
}

func (w *recordingEC2InstanceNodeWriter) WriteEC2InstanceNodes(
	_ context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	w.calls++
	w.rows = append(w.rows, rows...)
	w.evidenceSource = evidenceSource
	return w.err
}

func ec2InstancePostureEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.EC2InstancePostureFactKind,
		FactID:   "fact-" + payloadcore.AnyToString(payload["instance_id"]),
		Payload:  payload,
	}
}

func sampleEC2PosturePayload(instanceID string) map[string]any {
	return map[string]any{
		"account_id":                  "111122223333",
		"region":                      "us-east-1",
		"service_kind":                "ec2",
		"resource_type":               "aws_ec2_instance",
		"arn":                         "arn:aws:ec2:us-east-1:111122223333:instance/" + instanceID,
		"instance_id":                 instanceID,
		"state":                       "running",
		"imds_v2_required":            true,
		"imds_http_endpoint":          "enabled",
		"imds_http_put_hop_limit":     float64(1), // facts deserialize numbers as float64
		"user_data_present":           false,
		"detailed_monitoring_enabled": false,
		"ebs_optimized":               true,
		"public_ip_associated":        true,
		"public_ip_address":           "203.0.113.10", // present on the fact, must NOT reach the node
		"instance_profile_arn":        "arn:aws:iam::111122223333:instance-profile/app",
		"tenancy":                     "default",
		"nitro_enclave_enabled":       false,
		"correlation_anchors":         []any{instanceID},
	}
}
