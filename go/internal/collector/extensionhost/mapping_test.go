// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// TestCollectedGenerationForwardsTrustedDeletion proves the host does not
// drop the extension's trusted-deletion attestation: an extension generation
// that attests legitimate emptiness reaches the commit path, so the store's
// successor-emptiness backstop honors it instead of refusing every tick.
func TestCollectedGenerationForwardsTrustedDeletion(t *testing.T) {
	t.Parallel()

	collect := func(t *testing.T, trusted bool) bool {
		t.Helper()
		item := testWorkItem()
		result := completeResult(item, testSDKFact(item))
		result.Generation.TrustedDeletion = trusted
		source := mustNewSource(t, &recordingRunner{result: result}, nil)
		collected, ok, err := source.NextClaimed(context.Background(), item)
		if err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("NextClaimed() ok = false, want true")
		}
		return collected.Generation.TrustedDeletion
	}
	if got := collect(t, false); got {
		t.Fatal("TrustedDeletion = true without attestation, want false")
	}
	if got := collect(t, true); !got {
		t.Fatal("TrustedDeletion = false with attestation, want true")
	}
}

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
