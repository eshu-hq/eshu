// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildProjectionQueuesS3InternetExposureMaterializationFromPosture(t *testing.T) {
	t.Parallel()

	scopeValue, generation := s3LogsToScopeAndGeneration()
	envelopes := []facts.Envelope{
		s3PostureIntentEnvelope("fact-posture-1", scopeValue.ScopeID, generation.GenerationID, ""),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainS3InternetExposureMaterialization)
	if got, want := intent.EntityKey, "aws_resource_materialization:aws:111111111111:us-east-1:s3"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-posture-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first posture fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueS3InternetExposureWithoutPosture(t *testing.T) {
	t.Parallel()

	scopeValue, generation := s3LogsToScopeAndGeneration()
	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainS3InternetExposureMaterialization {
			t.Fatal("unexpected s3_internet_exposure_materialization intent without posture facts")
		}
	}
}
