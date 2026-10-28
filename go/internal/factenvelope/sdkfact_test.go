// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factenvelope

import (
	"reflect"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSDKFactFromInternalRoundTrip proves the reverse mapper is the exact
// inverse of InternalFromSDKFact on every wire-carried field, so analyzer
// byte accounting measures the true SDK wire bytes of internal envelopes.
func TestSDKFactFromInternalRoundTrip(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 8, 12, 0, 0, 0, time.UTC)
	original := sdkcollector.Fact{
		Kind:             "dev.eshu.examples.scorecard.check",
		SchemaVersion:    "1.0.0",
		StableKey:        "scorecard:repo-eshu",
		SourceConfidence: sdkcollector.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Tombstone:        true,
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: "git",
			ScopeID:      "repo-eshu",
			GenerationID: "generation-1",
			FactKey:      "scorecard:repo-eshu",
			URI:          "git://repo-eshu",
			RecordID:     "scorecard-record",
		},
		Payload: map[string]any{
			"score": 98,
			"state": "pass",
		},
		Redactions: []sdkcollector.Redaction{{Field: "token", Reason: "credential"}},
	}
	envelope := InternalFromSDKFact(original, InternalEnvelopeOptions{
		ComponentID:   "dev.eshu.examples.scorecard",
		ScopeID:       "repo-eshu",
		GenerationID:  "generation-1",
		CollectorKind: "git",
		FencingToken:  7,
	})
	// The durable envelope has no redaction field; the reverse projection
	// carries none back.
	roundTripped, err := SDKFactFromInternal(envelope)
	if err != nil {
		t.Fatalf("SDKFactFromInternal() error = %v, want nil", err)
	}
	if roundTripped.Kind != original.Kind ||
		roundTripped.SchemaVersion != original.SchemaVersion ||
		roundTripped.StableKey != original.StableKey ||
		roundTripped.SourceConfidence != original.SourceConfidence ||
		!roundTripped.ObservedAt.Equal(original.ObservedAt) ||
		roundTripped.Tombstone != original.Tombstone {
		t.Fatalf("SDKFactFromInternal() = %+v, want wire fields of %+v", roundTripped, original)
	}
	if roundTripped.SourceRef != original.SourceRef {
		t.Fatalf("SDKFactFromInternal() source_ref = %+v, want %+v", roundTripped.SourceRef, original.SourceRef)
	}
	if !reflect.DeepEqual(roundTripped.Payload, original.Payload) {
		t.Fatalf("SDKFactFromInternal() payload = %#v, want %#v", roundTripped.Payload, original.Payload)
	}
}

// TestSDKFactFromInternalRejectsUnknownConfidence proves the mapper fails
// closed when the envelope carries a confidence the wire cannot represent,
// instead of emitting a fact the SDK validator would reject downstream.
func TestSDKFactFromInternalRejectsUnknownConfidence(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind:         "dev.eshu.examples.scorecard.check",
		StableFactKey:    "scorecard:repo-eshu",
		SchemaVersion:    "1.0.0",
		SourceConfidence: "bogus-confidence",
		ObservedAt:       time.Date(2026, time.July, 8, 12, 0, 0, 0, time.UTC),
		Payload:          map[string]any{"score": 98},
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			ScopeID:        "repo-eshu",
			GenerationID:   "generation-1",
			FactKey:        "scorecard:repo-eshu",
			SourceURI:      "git://repo-eshu",
			SourceRecordID: "scorecard-record",
		},
	}
	if _, err := SDKFactFromInternal(envelope); err == nil {
		t.Fatal("SDKFactFromInternal(bogus confidence) error = nil, want non-nil")
	}
}
