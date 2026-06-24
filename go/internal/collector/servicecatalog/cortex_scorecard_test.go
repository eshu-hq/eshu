// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestCortexScorecardEnvelopesEmitsCarriedFacts proves the scorecard descriptor
// produces scorecard_definition (per rule) and scorecard_result (per entity)
// facts that pass the schema-version gate, anchor on provider+entity_ref, and
// never carry canonical ids.
func TestCortexScorecardEnvelopesEmitsCarriedFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_scorecard.yaml")
	envelopes, err := CortexScorecardEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexScorecardEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// Two rules -> two definition facts; two declared results -> two result facts.
	assertKindCount(t, byKind, facts.ServiceCatalogScorecardDefinitionFactKind, 2)
	assertKindCount(t, byKind, facts.ServiceCatalogScorecardResultFactKind, 2)

	def := findScorecardDefinition(t, envelopes, "has-runbook")
	assertPayload(t, def.Payload, "provider", string(ProviderCortex))
	assertPayload(t, def.Payload, "scorecard_tag", "production-readiness")
	assertPayload(t, def.Payload, "rule_identifier", "has-runbook")
	assertPayload(t, def.Payload, "level", "Bronze")

	result := findScorecardResult(t, envelopes, "service:cortex/checkout-api")
	assertPayload(t, result.Payload, "provider", string(ProviderCortex))
	assertPayload(t, result.Payload, "entity_ref", "service:cortex/checkout-api")
	assertPayload(t, result.Payload, "scorecard_tag", "production-readiness")
	assertPayload(t, result.Payload, "level", "Gold")
	// Never mint canonical identity from a scorecard result.
	assertBlank(t, result.Payload, "service_id")
	assertBlank(t, result.Payload, "workload_id")

	for _, envelope := range envelopes {
		if _, ok := facts.ServiceCatalogSchemaVersion(envelope.FactKind); !ok {
			t.Fatalf("emitted unexpected fact kind %q", envelope.FactKind)
		}
		if envelope.SchemaVersion != facts.ServiceCatalogSchemaVersionV1 {
			t.Fatalf("fact %q schema_version = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.ServiceCatalogSchemaVersionV1)
		}
	}
}

// TestCortexScorecardResultsDoNotChangeCorrelation proves scorecard facts are
// carried-only: feeding them alongside entity facts must not alter any entity's
// reducer outcome, because the reducer index does not consume scorecards yet.
func TestCortexScorecardResultsDoNotChangeCorrelation(t *testing.T) {
	t.Parallel()

	catalog, err := CortexManifestEnvelopes(readFixture(t, "testdata/cortex_catalog.yaml"), cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}
	scorecards, err := CortexScorecardEnvelopes(readFixture(t, "testdata/cortex_scorecard.yaml"), cortexContext())
	if err != nil {
		t.Fatalf("CortexScorecardEnvelopes() error = %v", err)
	}
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout", "https://github.com/eshu-hq/checkout-api", false),
	}

	withoutScorecards := decisionsByEntity(reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...)))
	combined := append(append([]facts.Envelope{}, catalog...), scorecards...)
	withScorecards := decisionsByEntity(reducer.BuildServiceCatalogCorrelationDecisions(append(combined, repos...)))

	if len(withoutScorecards) != len(withScorecards) {
		t.Fatalf("scorecard facts changed decision count: %d vs %d", len(withoutScorecards), len(withScorecards))
	}
	for ref, before := range withoutScorecards {
		after, ok := withScorecards[ref]
		if !ok {
			t.Fatalf("entity %q lost its decision when scorecards were added", ref)
		}
		if before.Outcome != after.Outcome {
			t.Fatalf("entity %q outcome drifted with scorecards: %q -> %q", ref, before.Outcome, after.Outcome)
		}
	}
}

func TestCortexScorecardEmptyInputIsClean(t *testing.T) {
	t.Parallel()

	envelopes, err := CortexScorecardEnvelopes([]byte("\n# nothing\n"), cortexContext())
	if err != nil {
		t.Fatalf("CortexScorecardEnvelopes() empty error = %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("empty scorecard envelopes = %d, want 0", len(envelopes))
	}
}

// TestCortexNormalizeScorecardScore proves the score normalizer renders YAML
// scalar types into a stable string. Integral floats render without a decimal
// point, and out-of-range floats render as a full decimal string rather than an
// implementation-defined int64 round-trip, so re-emission stays idempotent.
func TestCortexNormalizeScorecardScore(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		score any
		want  string
	}{
		{"nil", nil, ""},
		{"string", "  Gold  ", "Gold"},
		{"int", 87, "87"},
		{"int64", int64(91), "91"},
		{"integral_float", 100.0, "100"},
		{"fractional_float", 87.5, "87.5"},
		{"zero_float", 0.0, "0"},
		{"negative_integral_float", -3.0, "-3"},
		// A float far beyond int64 range must not overflow into a wrong/unstable
		// value; it renders as a stable full-precision decimal string.
		{"out_of_range_float", 1e30, "1000000000000000000000000000000"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeScorecardScore(tc.score); got != tc.want {
				t.Fatalf("normalizeScorecardScore(%v) = %q, want %q", tc.score, got, tc.want)
			}
		})
	}
}

// --- cortex scorecard test helpers ---

func findScorecardDefinition(t *testing.T, envelopes []facts.Envelope, ruleIdentifier string) facts.Envelope {
	t.Helper()
	for i := range envelopes {
		if envelopes[i].FactKind == facts.ServiceCatalogScorecardDefinitionFactKind && envelopes[i].Payload["rule_identifier"] == ruleIdentifier {
			return envelopes[i]
		}
	}
	t.Fatalf("scorecard definition for rule %q not found", ruleIdentifier)
	return facts.Envelope{}
}

func findScorecardResult(t *testing.T, envelopes []facts.Envelope, entityRef string) facts.Envelope {
	t.Helper()
	for i := range envelopes {
		if envelopes[i].FactKind == facts.ServiceCatalogScorecardResultFactKind && envelopes[i].Payload["entity_ref"] == entityRef {
			return envelopes[i]
		}
	}
	t.Fatalf("scorecard result for %q not found", entityRef)
	return facts.Envelope{}
}
