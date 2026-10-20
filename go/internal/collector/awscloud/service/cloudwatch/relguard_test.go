// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudwatch

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
)

// TestEmittedRelationshipsSatisfyGraphJoinContract feeds the relationships this
// scanner builds through the shared relguard runtime helper. It is the cheap,
// one-line adoption pattern every scanner test can copy: build the
// RelationshipObservation values, then assert the graph-join contract. The
// runtime layer catches the data-dependent target_type and ARN-keying defects
// the repo-level static guard cannot see, including the metric-stream-to-Firehose
// edge whose target_type previously did not match the type the kinesis scanner
// publishes (the #804 regression).
func TestEmittedRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	boundary := testBoundary()

	firehose, ok := metricStreamFirehoseRelationship(boundary, MetricStream{
		ARN:         "arn:aws:cloudwatch:us-east-1:123456789012:metric-stream/orders-stream",
		Name:        "orders-stream",
		FirehoseARN: "arn:aws:firehose:us-east-1:123456789012:deliverystream/cw-metrics",
	})
	if !ok {
		t.Fatal("metricStreamFirehoseRelationship did not emit an edge for a Firehose ARN")
	}
	relguard.AssertObservations(t, firehose)

	sns := alarmSNSRelationships(
		boundary,
		"arn:aws:cloudwatch:us-east-1:123456789012:alarm:high-cpu",
		"high-cpu",
		[]string{"arn:aws:sns:us-east-1:123456789012:on-call"},
		nil,
		nil,
	)
	if len(sns) == 0 {
		t.Fatal("alarmSNSRelationships did not emit an edge for an SNS action")
	}
	relguard.AssertObservations(t, sns...)
}
