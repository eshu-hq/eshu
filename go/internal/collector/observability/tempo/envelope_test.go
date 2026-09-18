// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestObservedTraceSignalEnvelopeRedactsTagValuesAndTraceQL(t *testing.T) {
	t.Parallel()

	ctx := EnvelopeContext{
		ScopeID:             "tempo-prod",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-1",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		SourceInstanceID:    "tempo-main",
	}
	signal := TraceSignal{
		ProviderObjectID:   "service-values",
		SignalKind:         SignalKindTagValues,
		TagScope:           "resource",
		TagName:            "resource.service.name",
		TagValueCount:      2,
		TagValueHashes:     []string{fingerprint("checkout-prod"), fingerprint("billing-prod")},
		Query:              `{ resource.service.name = "checkout-prod" }`,
		QueryRedacted:      true,
		DeclaredMatchState: MatchStateNotCompared,
	}

	env, err := NewObservedTraceSignalEnvelope(ctx, signal)
	if err != nil {
		t.Fatalf("NewObservedTraceSignalEnvelope() error = %v", err)
	}

	if env.FactKind != facts.ObservabilityObservedTraceSignalFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.ObservabilityObservedTraceSignalFactKind)
	}
	payload := env.Payload
	for _, forbidden := range []string{"checkout-prod", "billing-prod", "TraceQL", signal.Query} {
		assertPayloadOmitsString(t, payload, forbidden)
	}
	if payload["query_redacted"] != true {
		t.Fatalf("query_redacted = %#v, want true", payload["query_redacted"])
	}
	if got := payload["tag_value_count"]; got != 2 {
		t.Fatalf("tag_value_count = %#v, want 2", got)
	}
	hashes, ok := payload["tag_value_hashes"].([]string)
	if !ok || len(hashes) != 2 {
		t.Fatalf("tag_value_hashes = %#v, want two redacted hashes", payload["tag_value_hashes"])
	}
}

func TestCoverageWarningEnvelopePreservesHighCardinalityRejection(t *testing.T) {
	t.Parallel()

	ctx := EnvelopeContext{
		ScopeID:             "tempo-prod",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-1",
		ObservedAt:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		SourceInstanceID:    "tempo-main",
	}
	env, err := NewCoverageWarningEnvelope(ctx, Warning{
		ResourceClass: ResourceClassTraceSignal,
		ResourceID:    "tag:resource.service.name",
		Reason:        WarningHighCardinality,
	})
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v", err)
	}
	payload := env.Payload
	if got := payload["warning_kind"]; got != WarningHighCardinality {
		t.Fatalf("warning_kind = %#v, want %q", got, WarningHighCardinality)
	}
	if got := payload["outcome"]; got != OutcomeRejected {
		t.Fatalf("outcome = %#v, want %q", got, OutcomeRejected)
	}
	if got := payload["freshness_state"]; got != FreshnessUnknown {
		t.Fatalf("freshness_state = %#v, want %q", got, FreshnessUnknown)
	}
}
