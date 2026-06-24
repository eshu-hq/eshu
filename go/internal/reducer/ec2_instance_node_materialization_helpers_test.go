// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
		FactID:   "fact-" + anyToString(payload["instance_id"]),
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
