// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Extraction PREP mirror (#8857 AC3, in-tree path): the contracts the
// extracted collector must preserve — admission at schema 1.0.0, stable
// keys, generation binding, and credential redaction — proved against the
// current in-tree constructors, including hostile inputs.
//
// No-Observability-Change: existing eshu_dp_grafana_* signals cover this
// package; this test adds no metric, span, or pipeline stage.

func extractionPrepContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "grafana:prod",
		GenerationID:        "grafana:generation-9",
		CollectorInstanceID: "collector-grafana",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 16, 11, 0, 0, 0, time.UTC),
		SourceURI:           "https://grafana.example.test",
		SourceInstanceID:    "prod",
	}
}

func extractionPrepResource() Resource {
	return Resource{
		Class:          ResourceClassDashboard,
		ID:             42,
		UID:            "dashboard-uid-1",
		Title:          "Checkout overview",
		FolderUID:      "folder-uid-1",
		DatasourceType: "prometheus",
		URL:            "https://grafana.example.test/d/dashboard-uid-1",
		UpdatedAt:      time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC),
		FreshnessState: FreshnessCurrent,
		Outcome:        OutcomeObserved,
	}
}

func extractionPrepRule() AlertRule {
	return AlertRule{
		UID:            "alert-rule-uid-1",
		Title:          "High error rate",
		RuleGroup:      "payments",
		FolderUID:      "folder-uid-1",
		DatasourceUID:  "prometheus-uid-1",
		For:            "5m",
		FreshnessState: FreshnessCurrent,
		Outcome:        OutcomeObserved,
	}
}

func extractionPrepEnvelopes(t *testing.T, ctx EnvelopeContext) []facts.Envelope {
	t.Helper()

	source, err := NewSourceInstanceEnvelope(ctx, CollectionStats{Resources: 1, Rules: 1})
	if err != nil {
		t.Fatalf("NewSourceInstanceEnvelope() error = %v", err)
	}
	resource, err := NewObservedResourceEnvelope(ctx, extractionPrepResource())
	if err != nil {
		t.Fatalf("NewObservedResourceEnvelope() error = %v", err)
	}
	rule, err := NewObservedRuleEnvelope(ctx, extractionPrepRule())
	if err != nil {
		t.Fatalf("NewObservedRuleEnvelope() error = %v", err)
	}
	warning, err := NewCoverageWarningEnvelope(ctx, Warning{ResourceClass: ResourceClassDashboard, Reason: WarningRateLimited})
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v", err)
	}
	return []facts.Envelope{source, resource, rule, warning}
}

func TestExtractionPrepAllKindsAdmitAtV1(t *testing.T) {
	t.Parallel()

	kinds := []string{
		facts.ObservabilitySourceInstanceFactKind,
		facts.ObservabilityObservedDashboardFactKind,
		facts.ObservabilityObservedRuleFactKind,
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
	if err := facts.ValidateSchemaVersion(facts.ObservabilityObservedDashboardFactKind, "9.9.9"); err == nil {
		t.Error("ValidateSchemaVersion(observed_dashboard, 9.9.9) = nil, want incompatible-schema rejection")
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
	rebound.GenerationID = "grafana:generation-10"
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

	const sentinel = "PREP-SENTINEL-8857"
	ctx := extractionPrepContext()

	hostileResource := extractionPrepResource()
	hostileResource.URL = "https://svc:" + sentinel + "@grafana.example.test/d/uid?api_key=" + sentinel
	resourceEnv, err := NewObservedResourceEnvelope(ctx, hostileResource)
	if err != nil {
		t.Fatalf("NewObservedResourceEnvelope(hostile URL) error = %v", err)
	}
	hostileRule := extractionPrepRule()
	hostileRule.Model = map[string]any{"expr": "secret=" + sentinel}
	hostileRule.ContactPoint = "contact-" + sentinel
	hostileRule.NotificationURL = "https://hooks.example.test/" + sentinel
	ruleEnv, err := NewObservedRuleEnvelope(ctx, hostileRule)
	if err != nil {
		t.Fatalf("NewObservedRuleEnvelope(hostile model/contact) error = %v", err)
	}
	for _, env := range []facts.Envelope{resourceEnv, ruleEnv} {
		raw, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), sentinel) {
			t.Errorf("credential sentinel reached persisted envelope %q", env.FactKind)
		}
	}
	if resourceEnv.Payload["url_redacted"] != true {
		t.Error("resource url_redacted = false, want metadata-only redaction flag")
	}
	if ruleEnv.Payload["query_model_redacted"] != true {
		t.Error("rule query_model_redacted = false, want query-model redaction flag")
	}
	if ruleEnv.Payload["contact_point_redacted"] != true {
		t.Error("rule contact_point_redacted = false, want contact redaction flag")
	}
	if ruleEnv.Payload["notification_url_redacted"] != true {
		t.Error("rule notification_url_redacted = false, want notification-URL redaction flag")
	}
	// Positive control: benign provider identity survives so the absence
	// above is meaningful.
	benign := extractionPrepEnvelopes(t, ctx)
	raw, _ := json.Marshal(benign)
	if !strings.Contains(string(raw), "dashboard-uid-1") {
		t.Error("positive control: benign provider UID missing from persisted envelopes")
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
			if _, err := NewSourceInstanceEnvelope(ctx, CollectionStats{}); err == nil {
				t.Errorf("NewSourceInstanceEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
			if _, err := NewObservedResourceEnvelope(ctx, extractionPrepResource()); err == nil {
				t.Errorf("NewObservedResourceEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
			if _, err := NewObservedRuleEnvelope(ctx, extractionPrepRule()); err == nil {
				t.Errorf("NewObservedRuleEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
			if _, err := NewCoverageWarningEnvelope(ctx, Warning{Reason: WarningPartial}); err == nil {
				t.Errorf("NewCoverageWarningEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
		})
	}
	// Blank resource identity fails closed; blank warning reason degrades
	// to a partial-read warning instead of failing.
	ctx := extractionPrepContext()
	badResource := extractionPrepResource()
	badResource.UID = "  "
	badResource.Name = ""
	badResource.ID = 0
	if _, err := NewObservedResourceEnvelope(ctx, badResource); err == nil {
		t.Error("NewObservedResourceEnvelope(blank identity) = nil, want error")
	}
	blankClass := extractionPrepResource()
	blankClass.Class = "  "
	if _, err := NewObservedResourceEnvelope(ctx, blankClass); err == nil {
		t.Error("NewObservedResourceEnvelope(blank class) = nil, want error")
	}
	badRule := extractionPrepRule()
	badRule.UID = ""
	if _, err := NewObservedRuleEnvelope(ctx, badRule); err == nil {
		t.Error("NewObservedRuleEnvelope(blank uid) = nil, want error")
	}
	warning, err := NewCoverageWarningEnvelope(ctx, Warning{})
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope(blank) error = %v, want partial-degrade not fatal", err)
	}
	if warning.Payload["warning_kind"] != WarningPartial {
		t.Errorf("warning_kind = %v, want %q degrade", warning.Payload["warning_kind"], WarningPartial)
	}
}
