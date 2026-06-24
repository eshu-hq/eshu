// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildProjectionQueuesEC2InternetExposureMaterializationFromPosture(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2UsesProfileScopeAndGeneration()
	envelopes := []facts.Envelope{{
		FactID:        "fact-ec2-posture-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.EC2InstancePostureFactKind,
		SchemaVersion: facts.EC2InstancePostureSchemaVersionV1,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"instance_id":          "i-123",
			"public_ip_associated": true,
		},
	}}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainEC2InternetExposureMaterialization)
	if got, want := intent.EntityKey, "ec2_instance_node_materialization:aws:111122223333:us-east-1:ec2"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-ec2-posture-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first posture fact", got)
	}
}

func TestBuildProjectionDoesNotQueueEC2InternetExposureWithoutPosture(t *testing.T) {
	t.Parallel()

	scopeValue, generation := ec2UsesProfileScopeAndGeneration()
	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainEC2InternetExposureMaterialization {
			t.Fatal("unexpected ec2_internet_exposure_materialization intent without posture facts")
		}
	}
}
