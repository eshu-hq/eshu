// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticcode

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testConfig() Config {
	return Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-code-default",
			ProviderKind:      ProviderKindMock,
			ModelID:           "mock-model",
		},
		Now: func() time.Time { return time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC) },
	}
}

func testSpan() CodeSpanInput {
	return CodeSpanInput{
		ScopeID:      "git:repository:orders-api",
		GenerationID: "gen-1",
		SourceSystem: "git",
		RepositoryID: "orders-api",
		RelativePath: "main.go",
		SpanID:       "orders-api:main.go:1-40",
		CanonicalURI: "repo://orders-api/main.go#L1-L40",
		ContentHash:  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		LineStart:    1,
		LineEnd:      40,
		ObservedAt:   time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC),
	}
}

func testHint() HintInput {
	return HintInput{
		HintType:         "relationship",
		RelationshipKind: "calls",
		HintText:         "main appears to call the shared logging helper",
		Subject: facts.SemanticCodeEntityRef{
			EntityKind: "function",
			EntityID:   "entity:orders-api:main.go:main",
			LineStart:  10,
			LineEnd:    20,
		},
		Confidence: facts.SemanticConfidenceMedium,
	}
}

// TestEmitProducesAnAdmissibleCodeHintFact is the regression guard for issue
// #5693: semantic.code_hint had a read contract, a payload schema, an MCP tool
// and a capability row, and no producer anywhere in the runtime, so every
// deployed read returned a correctly-formed empty list.
func TestEmitProducesAnAdmissibleCodeHintFact(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(testConfig())
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}

	envelopes, err := emitter.Emit(context.Background(), testSpan(), []HintInput{testHint()})
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("len(envelopes) = %d, want 1", len(envelopes))
	}

	envelope := envelopes[0]
	if envelope.FactKind != facts.SemanticCodeHintFactKind {
		t.Errorf("FactKind = %q, want %q", envelope.FactKind, facts.SemanticCodeHintFactKind)
	}
	if envelope.SchemaVersion != facts.SemanticFactSchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.SemanticFactSchemaVersion)
	}
	if envelope.StableFactKey == "" || envelope.FactID != envelope.StableFactKey {
		t.Errorf("FactID/StableFactKey = %q/%q, want a non-empty matching pair", envelope.FactID, envelope.StableFactKey)
	}
	if envelope.ScopeID != testSpan().ScopeID || envelope.GenerationID != testSpan().GenerationID {
		t.Errorf("scope/generation = %q/%q, want the span's", envelope.ScopeID, envelope.GenerationID)
	}
	if envelope.SourceRef.SourceURI != testSpan().CanonicalURI {
		t.Errorf("SourceRef.SourceURI = %q, want the span's canonical URI", envelope.SourceRef.SourceURI)
	}
}

// TestEmitStampsTheNonCanonicalPromotionBoundary is the property that keeps a
// hint from being read as a fact. A provider can be as confident as it likes;
// the payload must still say the hint needs deterministic corroboration before
// anything promotes it.
func TestEmitStampsTheNonCanonicalPromotionBoundary(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(testConfig())
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}

	hint := testHint()
	hint.Confidence = facts.SemanticConfidenceHigh
	envelopes, err := emitter.Emit(context.Background(), testSpan(), []HintInput{hint})
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	payload := envelopes[0].Payload
	if got := payload["promotion_policy"]; got != facts.SemanticPromotionRequiresDeterministicEvidence {
		t.Errorf("promotion_policy = %v, want %q even for a high-confidence hint", got, facts.SemanticPromotionRequiresDeterministicEvidence)
	}
	if got := payload["corroboration_state"]; got != facts.SemanticCorroborationUncorroborated {
		t.Errorf("corroboration_state = %v, want %q when nothing has checked the hint", got, facts.SemanticCorroborationUncorroborated)
	}
}

// TestEmitIsDeterministicForTheSameProviderOutput keeps replay honest: the same
// span and hint must produce the same fact identity, or a re-run would double
// every hint instead of superseding it.
func TestEmitIsDeterministicForTheSameProviderOutput(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(testConfig())
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}

	first, err := emitter.Emit(context.Background(), testSpan(), []HintInput{testHint()})
	if err != nil {
		t.Fatalf("Emit() first error = %v", err)
	}
	second, err := emitter.Emit(context.Background(), testSpan(), []HintInput{testHint()})
	if err != nil {
		t.Fatalf("Emit() second error = %v", err)
	}
	if first[0].StableFactKey != second[0].StableFactKey {
		t.Errorf("StableFactKey = %q then %q, want identical", first[0].StableFactKey, second[0].StableFactKey)
	}

	// A changed source must NOT reuse the identity: a hint about code that has
	// since changed is a different hint, and reusing the key would let a stale
	// hint masquerade as current.
	movedSpan := testSpan()
	movedSpan.ContentHash = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	moved, err := emitter.Emit(context.Background(), movedSpan, []HintInput{testHint()})
	if err != nil {
		t.Fatalf("Emit() moved error = %v", err)
	}
	if moved[0].StableFactKey == first[0].StableFactKey {
		t.Error("StableFactKey unchanged after the source hash changed; a stale hint would supersede the fresh one")
	}
}

// TestEmitRejectsUntraceableInput proves the emitter fails closed rather than
// producing a hint nobody can follow back to code.
func TestEmitRejectsUntraceableInput(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(testConfig())
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}

	for name, mutate := range map[string]func(*CodeSpanInput){
		"no repository": func(s *CodeSpanInput) { s.RepositoryID = "" },
		"no path":       func(s *CodeSpanInput) { s.RelativePath = "" },
		"no span id":    func(s *CodeSpanInput) { s.SpanID = "" },
		"no uri":        func(s *CodeSpanInput) { s.CanonicalURI = "" },
		"no hash":       func(s *CodeSpanInput) { s.ContentHash = "" },
		"no generation": func(s *CodeSpanInput) { s.GenerationID = "" },
	} {
		span := testSpan()
		mutate(&span)
		if _, err := emitter.Emit(context.Background(), span, []HintInput{testHint()}); err == nil {
			t.Errorf("%s: Emit() error = nil, want a refusal", name)
		}
	}

	hintWithoutSubject := testHint()
	hintWithoutSubject.Subject.EntityID = ""
	if _, err := emitter.Emit(context.Background(), testSpan(), []HintInput{hintWithoutSubject}); err == nil {
		t.Error("hint with no subject entity: Emit() error = nil, want a refusal")
	}
}

// TestNewEmitterRejectsAnUnusableProfile keeps a misconfigured provider profile
// a startup error instead of a stream of facts that dead-letter one at a time.
func TestNewEmitterRejectsAnUnusableProfile(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*Config){
		"no profile id": func(c *Config) { c.Provider.ProviderProfileID = "" },
		"no kind":       func(c *Config) { c.Provider.ProviderKind = "" },
		"bad policy":    func(c *Config) { c.PolicyState = "sort-of-allowed" },
	} {
		config := testConfig()
		mutate(&config)
		if _, err := NewEmitter(config); err == nil {
			t.Errorf("%s: NewEmitter() error = nil, want a refusal", name)
		}
	}
}

// TestEmitDefaultsAreTheConservativeReading pins what an unset config means.
// Every default here claims less rather than more.
func TestEmitDefaultsAreTheConservativeReading(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(testConfig())
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	envelopes, err := emitter.Emit(context.Background(), testSpan(), []HintInput{testHint()})
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	payload := envelopes[0].Payload
	for key, want := range map[string]string{
		"policy_state":    facts.SemanticPolicyAllowed,
		"redaction_state": facts.SemanticRedactionSkippedNoSensitiveContent,
		"freshness_state": facts.SemanticFreshnessFresh,
	} {
		if got := payload[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if got := envelopes[0].CollectorKind; got != CollectorKind {
		t.Errorf("CollectorKind = %q, want %q", got, CollectorKind)
	}
	if got := envelopes[0].SourceConfidence; got != facts.SourceConfidenceDerived {
		t.Errorf("SourceConfidence = %q, want %q", got, facts.SourceConfidenceDerived)
	}
}
