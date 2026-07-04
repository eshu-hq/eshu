// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDeduplicateEnvelopesPrefersHighestFencingToken(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		testDedupEnvelope("dup-1", 20, "newer"),
		testDedupEnvelope("unique-1", 1, "unique"),
		testDedupEnvelope("dup-1", 10, "stale"),
	}

	deduped := deduplicateEnvelopes(envelopes)

	if got, want := len(deduped), 2; got != want {
		t.Fatalf("len(deduped) = %d, want %d", got, want)
	}
	if got, want := deduped[0].FactID, "dup-1"; got != want {
		t.Fatalf("deduped[0].FactID = %q, want %q", got, want)
	}
	if got, want := deduped[0].FencingToken, int64(20); got != want {
		t.Fatalf("deduped[0].FencingToken = %d, want %d", got, want)
	}
	if got, want := deduped[0].Payload["version"], "newer"; got != want {
		t.Fatalf("deduped[0] payload version = %v, want %q", got, want)
	}
	if got, want := deduped[1].FactID, "unique-1"; got != want {
		t.Fatalf("deduped[1].FactID = %q, want %q", got, want)
	}
}

func TestDeduplicateEnvelopesKeepsLastPositionOnFencingTokenTie(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		testDedupEnvelope("dup-1", 7, "first"),
		testDedupEnvelope("unique-1", 1, "unique"),
		testDedupEnvelope("dup-1", 7, "last"),
	}

	deduped := deduplicateEnvelopes(envelopes)

	if got, want := len(deduped), 2; got != want {
		t.Fatalf("len(deduped) = %d, want %d", got, want)
	}
	if got, want := deduped[0].FactID, "unique-1"; got != want {
		t.Fatalf("deduped[0].FactID = %q, want %q", got, want)
	}
	if got, want := deduped[1].FactID, "dup-1"; got != want {
		t.Fatalf("deduped[1].FactID = %q, want %q", got, want)
	}
	if got, want := deduped[1].Payload["version"], "last"; got != want {
		t.Fatalf("deduped[1] payload version = %v, want %q", got, want)
	}
}

func testDedupEnvelope(factID string, fencingToken int64, version string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "repository",
		StableFactKey: "repository:" + factID,
		FencingToken:  fencingToken,
		ObservedAt:    time.Date(2026, time.July, 4, 17, 0, 0, 0, time.UTC),
		Payload:       map[string]any{"version": version},
		SourceRef: facts.Ref{
			SourceSystem: "git",
			FactKey:      factID,
		},
	}
}
