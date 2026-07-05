// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// observabilityFactWithout builds an observability envelope whose payload omits
// the named key, so a test can exercise the missing-required-field dead-letter
// path. Unlike the observabilityFact helper it never injects source_instance_id,
// scope_id, or generation_id, so the caller fully controls the payload.
func observabilityFactWithout(factID, kind string, payload map[string]any) facts.Envelope {
	version, _ := facts.ObservabilitySchemaVersion(kind)
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       "scope-observability",
		GenerationID:  "generation-observability",
		FactKind:      kind,
		SchemaVersion: version,
		StableFactKey: kind + ":" + factID,
		CollectorKind: "observability-test",
		ObservedAt:    time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC),
		Payload:       payload,
	}
}

// TestObservabilityCoverageMetadataQuarantinesMissingSourceInstanceID proves the
// Wave 4e accuracy guarantee: an observability metadata fact missing its
// required source_instance_id anchor dead-letters as a per-fact input_invalid
// quarantine — it is skipped and returned in the quarantine slice, no coverage
// decision (correlation row) is emitted for it — while a valid sibling fact in
// the same batch still classifies into a decision. Before the typed-decode
// migration the missing anchor was invisible: the raw payloadString read
// returned "" and the fact still produced a coverage decision keyed on the
// StableFactKey fallback, silently projecting a source-less correlation row.
func TestObservabilityCoverageMetadataQuarantinesMissingSourceInstanceID(t *testing.T) {
	t.Parallel()

	valid := observabilityFactWithout("valid-target", facts.ObservabilityObservedTargetFactKind, map[string]any{
		"source_instance_id":  "prometheus:prod",
		"source_class":        "observed",
		"source_kind":         "prometheus",
		"provider":            "prometheus",
		"resource_class":      "target",
		"provider_object_uid": "checkout-service:9090",
		"outcome":             "observed",
		"freshness_state":     "current",
	})
	// Same shape, but missing the required source_instance_id anchor — a
	// malformed collector emission.
	malformed := observabilityFactWithout("malformed-target", facts.ObservabilityObservedTargetFactKind, map[string]any{
		"source_class":        "observed",
		"source_kind":         "prometheus",
		"provider":            "prometheus",
		"resource_class":      "target",
		"provider_object_uid": "orphaned-service:9090",
		"outcome":             "observed",
		"freshness_state":     "current",
	})

	decisions, quarantined, err := classifyObservabilityMetadataEvidence([]facts.Envelope{valid, malformed})
	if err != nil {
		t.Fatalf("classifyObservabilityMetadataEvidence() error = %v, want nil (a missing required field is a per-fact quarantine, not a fatal error)", err)
	}

	if len(quarantined) != 1 {
		t.Fatalf("quarantined count = %d, want 1 (the malformed fact)", len(quarantined))
	}
	q := quarantined[0]
	if q.factID != "malformed-target" {
		t.Fatalf("quarantined fact id = %q, want malformed-target", q.factID)
	}
	if q.factKind != facts.ObservabilityObservedTargetFactKind {
		t.Fatalf("quarantined fact kind = %q, want %q", q.factKind, facts.ObservabilityObservedTargetFactKind)
	}
	if q.field != "source_instance_id" {
		t.Fatalf("quarantined missing field = %q, want source_instance_id", q.field)
	}
	if q.classification != "input_invalid" {
		t.Fatalf("quarantined classification = %q, want input_invalid", q.classification)
	}

	// The valid sibling still classifies into exactly one decision, and the
	// malformed fact contributes none (no source-less correlation row).
	if len(decisions) != 1 {
		t.Fatalf("decision count = %d, want 1 (only the valid fact); decisions = %+v", len(decisions), decisions)
	}
	if got := decisions[0].ObservabilityObjectRef; got != "checkout-service:9090" {
		t.Fatalf("decision object ref = %q, want the valid fact's provider_object_uid", got)
	}
	for _, decision := range decisions {
		if decision.ObservabilityObjectRef == "orphaned-service:9090" {
			t.Fatalf("malformed fact produced a coverage decision %+v, want none (it must dead-letter, not project a source-less row)", decision)
		}
	}
}

// TestBuildObservabilityCoverageDecisionsPropagatesMetadataQuarantine proves the
// quarantine threads all the way up through BuildObservabilityCoverageDecisions
// (the entry point both the correlation and materialization handlers call), so a
// malformed metadata fact is surfaced to the handler's recordQuarantinedFacts
// dead-letter path rather than being swallowed inside the classifier.
func TestBuildObservabilityCoverageDecisionsPropagatesMetadataQuarantine(t *testing.T) {
	t.Parallel()

	malformed := observabilityFactWithout("malformed-dashboard", facts.ObservabilityObservedDashboardFactKind, map[string]any{
		"source_class":        "observed",
		"source_kind":         "grafana",
		"provider":            "grafana",
		"resource_class":      "dashboard",
		"provider_object_uid": "orphaned-dashboard",
		"outcome":             "observed",
		"freshness_state":     "current",
	})

	decisions, quarantined, err := BuildObservabilityCoverageDecisions([]facts.Envelope{malformed})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}
	if len(quarantined) != 1 || quarantined[0].field != "source_instance_id" {
		t.Fatalf("quarantined = %+v, want one entry naming source_instance_id", quarantined)
	}
	for _, decision := range decisions {
		if decision.ObservabilityObjectRef == "orphaned-dashboard" {
			t.Fatalf("malformed fact produced decision %+v, want none", decision)
		}
	}
}
