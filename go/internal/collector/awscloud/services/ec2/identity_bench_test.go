// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"fmt"
	"testing"
)

// BenchmarkInstanceIdentityEnvelopesFleetScale measures the #5448 per-instance
// identity-fact emission cost at a fleet-scale instance count. It is the
// collector-side half of the fleet-scale fan-out proof: every instance now
// emits ONE additional aws_resource identity fact plus (when an AMI id is
// present) ONE aws_relationship fact, on top of the existing
// ec2_instance_posture fact. Both additions are O(1) per instance with no new
// AWS API call, so total cost must stay linear in instance count with no
// per-instance fan-out.
func BenchmarkInstanceIdentityEnvelopesFleetScale(b *testing.B) {
	const instanceCount = 5000
	boundary := testBoundary()
	instances := make([]Instance, 0, instanceCount)
	for i := 0; i < instanceCount; i++ {
		instances = append(instances, Instance{
			ID:      fmt.Sprintf("i-%016d", i),
			ARN:     fmt.Sprintf("arn:aws:ec2:us-east-1:123456789012:instance/i-%016d", i),
			State:   "running",
			ImageID: fmt.Sprintf("ami-%016d", i%50), // a realistic small AMI-reuse ratio
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		envelopeCount := 0
		for _, instance := range instances {
			envelopes, err := instanceIdentityEnvelopes(boundary, instance)
			if err != nil {
				b.Fatalf("instanceIdentityEnvelopes() error = %v, want nil", err)
			}
			envelopeCount += len(envelopes)
		}
		// One aws_resource identity fact + one aws_relationship fact per
		// instance (every synthetic instance here carries a non-blank AMI id).
		if want := instanceCount * 2; envelopeCount != want {
			b.Fatalf("envelopeCount = %d, want %d", envelopeCount, want)
		}
	}
}
