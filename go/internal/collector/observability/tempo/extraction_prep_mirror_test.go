// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Extraction PREP mirror (#8867 AC3, in-tree path): the contracts the
// extracted collector must preserve — admission at schema 1.0.0, stable
// keys, generation binding, and credential redaction — proved against the
// current in-tree constructors, including hostile inputs.
//
// No-Observability-Change: existing eshu_dp_tempo_* signals cover this
// package; this test adds no metric, span, or pipeline stage.

func extractionPrepContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "tempo:prod",
		GenerationID:        "tempo:generation-9",
		CollectorInstanceID: "collector-tempo",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 16, 11, 0, 0, 0, time.UTC),
		SourceInstanceID:    "prod",
	}
}

func extractionPrepSource() SourceInstance {
	return SourceInstance{
		Provider:         ProviderTempo,
		SourceInstanceID: "prod",
		TenantPresent:    true,
		TenantRedacted:   true,
	}
}

func extractionPrepTagSet() TraceSignal {
	return TraceSignal{
		ProviderObjectID:   "tagset-prod",
		SignalKind:         SignalKindTagSet,
		TagKeys:            []string{"resource.service.name", "span.http.route"},
		DeclaredMatchState: MatchStateNotCompared,
		FreshnessState:     FreshnessCurrent,
		Outcome:            OutcomeObserved,
	}
}

func extractionPrepTagValues() TraceSignal {
	return TraceSignal{
		ProviderObjectID:   "tagvalues-prod",
		SignalKind:         SignalKindTagValues,
		TagScope:           "resource",
		TagName:            "resource.service.name",
		TagValueCount:      2,
		TagValueHashes:     []string{fingerprint("checkout"), fingerprint("billing")},
		QueryRedacted:      true,
		DeclaredMatchState: MatchStateNotCompared,
		FreshnessState:     FreshnessCurrent,
		Outcome:            OutcomeObserved,
		ManuallyCreated:    true,
	}
}

func extractionPrepEnvelopes(t *testing.T, ctx EnvelopeContext) []facts.Envelope {
	t.Helper()

	source, err := NewSourceInstanceEnvelope(ctx, extractionPrepSource(), CollectionStats{Signals: 2, Warnings: 1})
	if err != nil {
		t.Fatalf("NewSourceInstanceEnvelope() error = %v", err)
	}
	tagSet, err := NewObservedTraceSignalEnvelope(ctx, extractionPrepTagSet())
	if err != nil {
		t.Fatalf("NewObservedTraceSignalEnvelope(tag set) error = %v", err)
	}
	tagValues, err := NewObservedTraceSignalEnvelope(ctx, extractionPrepTagValues())
	if err != nil {
		t.Fatalf("NewObservedTraceSignalEnvelope(tag values) error = %v", err)
	}
	warning, err := NewCoverageWarningEnvelope(ctx, Warning{ResourceClass: ResourceClassTraceSignal, Reason: WarningRateLimited})
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v", err)
	}
	return []facts.Envelope{source, tagSet, tagValues, warning}
}

func TestExtractionPrepAllKindsAdmitAtV1(t *testing.T) {
	t.Parallel()

	kinds := []string{
		facts.ObservabilitySourceInstanceFactKind,
		facts.ObservabilityObservedTraceSignalFactKind,
		facts.ObservabilityCoverageWarningFactKind,
	}
	for _, kind := range kinds {
		version, ok := facts.ObservabilitySchemaVersion(kind)
		if !ok || version != facts.ObservabilitySchemaVersionV1 {
			t.Errorf("ObservabilitySchemaVersion(%q) = %q, %v; want 1.0.0, true", kind, version, ok)
		}
		if err := facts.ValidateSchemaVersion(kind, facts.ObservabilitySchemaVersionV1); err != nil {
			t.Errorf("ValidateSchemaVersion(%q, 1.0.0) error = %v, want nil", kind, err)
		}
	}
	for _, env := range extractionPrepEnvelopes(t, extractionPrepContext()) {
		if env.SchemaVersion != facts.ObservabilitySchemaVersionV1 {
			t.Errorf("envelope %q SchemaVersion = %q, want 1.0.0", env.FactKind, env.SchemaVersion)
		}
		if env.CollectorKind != CollectorKind {
			t.Errorf("envelope %q CollectorKind = %q, want %q", env.FactKind, env.CollectorKind, CollectorKind)
		}
		if env.SourceConfidence != facts.SourceConfidenceReported {
			t.Errorf("envelope %q SourceConfidence = %q, want reported", env.FactKind, env.SourceConfidence)
		}
	}
	if err := facts.ValidateSchemaVersion(facts.ObservabilityObservedTraceSignalFactKind, "9.9.9"); err == nil {
		t.Error("ValidateSchemaVersion(observed_trace_signal, 9.9.9) = nil, want incompatible-schema rejection")
	}
}

func TestExtractionPrepDeterminism(t *testing.T) {
	t.Parallel()

	first := extractionPrepEnvelopes(t, extractionPrepContext())
	second := extractionPrepEnvelopes(t, extractionPrepContext())
	if len(first) != len(second) {
		t.Fatalf("len(first) = %d, len(second) = %d, want equal", len(first), len(second))
	}
	for i := range first {
		if first[i].StableFactKey != second[i].StableFactKey {
			t.Errorf("envelope[%d] StableFactKey not deterministic: %q vs %q", i, first[i].StableFactKey, second[i].StableFactKey)
		}
		if first[i].FactID != second[i].FactID {
			t.Errorf("envelope[%d] FactID not deterministic: %q vs %q", i, first[i].FactID, second[i].FactID)
		}
	}
}

func TestExtractionPrepGenerationRebind(t *testing.T) {
	t.Parallel()

	first := extractionPrepEnvelopes(t, extractionPrepContext())
	rebound := extractionPrepContext()
	rebound.GenerationID = "tempo:generation-10"
	rebound.FencingToken = 8
	second := extractionPrepEnvelopes(t, rebound)
	if len(first) != len(second) {
		t.Fatalf("len(first) = %d, len(second) = %d, want equal", len(first), len(second))
	}
	for i := range first {
		// Stable keys bind the generation: a rebind must change identity,
		// never silently reuse the prior generation's key.
		if first[i].StableFactKey == second[i].StableFactKey {
			t.Errorf("envelope[%d] StableFactKey identical across generations; generation must rebind identity", i)
		}
		if first[i].FactID == second[i].FactID {
			t.Errorf("envelope[%d] FactID identical across generations; generation must rebind identity", i)
		}
		if second[i].GenerationID != rebound.GenerationID || second[i].FencingToken != 8 {
			t.Errorf("envelope[%d] generation binding lost: %+v", i, second[i])
		}
		// Provider identity survives the rebind.
		if second[i].SourceRef.SourceSystem != CollectorKind {
			t.Errorf("envelope[%d] SourceSystem = %q, want %q", i, second[i].SourceRef.SourceSystem, CollectorKind)
		}
	}
}

func TestExtractionPrepRedactionSentinel(t *testing.T) {
	t.Parallel()

	const sentinel = "PREP-SENTINEL-8867"
	ctx := extractionPrepContext()

	hostileValues := extractionPrepTagValues()
	hostileValues.Query = `{ resource.service.name = "` + sentinel + `" }`
	hostileValues.QueryRedacted = true
	hostileValues.TagValueHashes = []string{fingerprint(sentinel)}
	signalEnv, err := NewObservedTraceSignalEnvelope(ctx, hostileValues)
	if err != nil {
		t.Fatalf("NewObservedTraceSignalEnvelope(hostile TraceQL/value) error = %v", err)
	}
	hostileSource := extractionPrepSource()
	hostileSource.TenantFingerprint = fingerprint(sentinel)
	sourceEnv, err := NewSourceInstanceEnvelope(ctx, hostileSource, CollectionStats{})
	if err != nil {
		t.Fatalf("NewSourceInstanceEnvelope(hostile tenant) error = %v", err)
	}
	for _, env := range []facts.Envelope{signalEnv, sourceEnv} {
		raw, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), sentinel) {
			t.Errorf("credential sentinel reached persisted envelope %q", env.FactKind)
		}
	}
	if signalEnv.Payload["query_redacted"] != true {
		t.Error("signal query_redacted = false, want TraceQL redaction flag")
	}
	// Positive control: benign provider identity survives so the absence
	// above is meaningful.
	benign := extractionPrepEnvelopes(t, ctx)
	raw, _ := json.Marshal(benign)
	if !strings.Contains(string(raw), "tagset-prod") {
		t.Error("positive control: benign provider object ID missing from persisted envelopes")
	}
}

func TestExtractionPrepHostileInputsFailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*EnvelopeContext)
	}{
		{"blank scope", func(c *EnvelopeContext) { c.ScopeID = "  " }},
		{"blank generation", func(c *EnvelopeContext) { c.GenerationID = "" }},
		{"blank collector instance", func(c *EnvelopeContext) { c.CollectorInstanceID = "" }},
		{"blank source instance", func(c *EnvelopeContext) { c.SourceInstanceID = "" }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := extractionPrepContext()
			tt.mutate(&ctx)
			if _, err := NewSourceInstanceEnvelope(ctx, extractionPrepSource(), CollectionStats{}); err == nil {
				t.Errorf("NewSourceInstanceEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
			if _, err := NewObservedTraceSignalEnvelope(ctx, extractionPrepTagSet()); err == nil {
				t.Errorf("NewObservedTraceSignalEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
			if _, err := NewCoverageWarningEnvelope(ctx, Warning{Reason: WarningPartial}); err == nil {
				t.Errorf("NewCoverageWarningEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
		})
	}
	// Blank signal identity fails closed; blank warning reason degrades
	// to a partial-read warning instead of failing.
	ctx := extractionPrepContext()
	badSignal := extractionPrepTagSet()
	badSignal.ProviderObjectID = "  "
	if _, err := NewObservedTraceSignalEnvelope(ctx, badSignal); err == nil {
		t.Error("NewObservedTraceSignalEnvelope(blank identity) = nil, want error")
	}
	warning, err := NewCoverageWarningEnvelope(ctx, Warning{})
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope(blank) error = %v, want partial-degrade not fatal", err)
	}
	if warning.Payload["warning_kind"] != WarningPartial {
		t.Errorf("warning_kind = %v, want %q degrade", warning.Payload["warning_kind"], WarningPartial)
	}
}
