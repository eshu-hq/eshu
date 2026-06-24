// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestSourceFactIDIgnoresSourceConfidenceMetadata(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	reported := testSDKFact(item)
	inferred := reported
	inferred.SourceConfidence = sdkcollector.SourceConfidenceInferred

	reportedSource := mustNewSource(t, &recordingRunner{result: completeResult(item, reported)}, nil)
	reportedCollected, ok, err := reportedSource.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("reported NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("reported NextClaimed() ok = false, want true")
	}

	inferredSource := mustNewSource(t, &recordingRunner{result: completeResult(item, inferred)}, nil)
	inferredCollected, ok, err := inferredSource.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("inferred NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("inferred NextClaimed() ok = false, want true")
	}

	reportedEnvelope := collectFacts(t, reportedCollected)[0]
	inferredEnvelope := collectFacts(t, inferredCollected)[0]
	if reportedEnvelope.StableFactKey != inferredEnvelope.StableFactKey {
		t.Fatalf("stable keys differ: %q != %q", reportedEnvelope.StableFactKey, inferredEnvelope.StableFactKey)
	}
	if reportedEnvelope.FactID != inferredEnvelope.FactID {
		t.Fatalf("FactID changed with source confidence: %q != %q", reportedEnvelope.FactID, inferredEnvelope.FactID)
	}
}
