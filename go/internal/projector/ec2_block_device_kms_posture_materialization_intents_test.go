// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildProjectionQueuesEC2BlockDeviceKMSPostureMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2UsesProfileScopeAndGeneration()
	envelopes := []facts.Envelope{
		ec2UsesProfileIntentEnvelope("fact-posture-1", scopeValue.ScopeID, generation.GenerationID, "i-aaa", ""),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainEC2BlockDeviceKMSPostureMaterialization)
	if got, want := intent.EntityKey, "ec2_block_device_kms_posture_materialization:aws:111122223333:us-east-1:ec2"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-posture-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first posture fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesEC2BlockDeviceKMSPostureForNoBlockDevices(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2UsesProfileScopeAndGeneration()
	envelopes := []facts.Envelope{
		ec2UsesProfileIntentEnvelope("fact-no-devices", scopeValue.ScopeID, generation.GenerationID, "i-empty", ""),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainEC2BlockDeviceKMSPostureMaterialization)
	if got, want := intent.FactID, "fact-no-devices"; got != want {
		t.Fatalf("intent.FactID = %q, want no-block-device posture fact", got)
	}
}

func TestBuildProjectionDoesNotQueueEC2BlockDeviceKMSPostureWithoutPosture(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2UsesProfileScopeAndGeneration()
	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainEC2BlockDeviceKMSPostureMaterialization {
			t.Fatal("unexpected ec2_block_device_kms_posture_materialization intent without EC2 posture")
		}
	}
}
