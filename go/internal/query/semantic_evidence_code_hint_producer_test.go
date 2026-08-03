// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/semanticcode"
)

// emitCodeHintRow runs the real producer and shapes its envelope the way the
// Postgres store hands a fact row to the read model, so this test exercises the
// actual producer payload rather than a hand-written approximation of it.
func emitCodeHintRow(t *testing.T) map[string]any {
	t.Helper()

	emitter, err := semanticcode.NewEmitter(semanticcode.Config{
		Provider: semanticcode.ProviderProfile{
			ProviderProfileID: "semantic-code-default",
			ProviderKind:      semanticcode.ProviderKindMock,
		},
		Now: func() time.Time { return time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}

	envelopes, err := emitter.Emit(context.Background(), semanticcode.CodeSpanInput{
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
	}, []semanticcode.HintInput{{
		HintType:         "relationship",
		RelationshipKind: "calls",
		HintText:         "main appears to call the shared logging helper",
		Subject: facts.SemanticCodeEntityRef{
			EntityKind: "function",
			EntityID:   "entity:orders-api:main.go:main",
		},
		Confidence: facts.SemanticConfidenceMedium,
	}})
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("len(envelopes) = %d, want 1", len(envelopes))
	}

	envelope := envelopes[0]
	return map[string]any{
		"fact_id":       envelope.FactID,
		"fact_kind":     envelope.FactKind,
		"scope_id":      envelope.ScopeID,
		"generation_id": envelope.GenerationID,
		"source_system": envelope.SourceRef.SourceSystem,
		"payload":       envelope.Payload,
	}
}

// TestCodeHintsReadAnswersFromTheRealProducer is the end-to-end half of issue
// #5693.
//
// The read handler has always been correct; what it never had was anything to
// read. So a handler test alone proved only that an empty answer is
// well-formed, which is exactly the shape the deployed surface returned. This
// runs the real semanticcode producer, hands its envelope to the read model the
// way the store does, and asserts the response actually carries the hint —
// including the non-canonical promotion state, which is what makes a code hint
// safe to expose at all.
func TestCodeHintsReadAnswersFromTheRealProducer(t *testing.T) {
	t.Parallel()

	row := semanticEvidencePublicRow(emitCodeHintRow(t))

	store := &fakeSemanticEvidenceStore{
		readModel: semanticEvidenceListReadModel{Rows: []map[string]any{row}},
	}
	handler := &SemanticEvidenceHandler{Content: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/semantic/code-hints?provider_profile_id=semantic-code-default&limit=25",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, _ := body["data"].(map[string]any)
	if data == nil {
		data = body
	}
	hints, _ := data["code_hints"].([]any)
	if len(hints) == 0 {
		t.Fatalf("code_hints is empty; the producer's fact did not survive to the answer: %s", w.Body.String())
	}

	hint, _ := hints[0].(map[string]any)
	if got := hint["fact_kind"]; got != facts.SemanticCodeHintFactKind {
		t.Errorf("fact_kind = %v, want %q", got, facts.SemanticCodeHintFactKind)
	}
	if got := hint["truth_basis"]; got != "code_hint" {
		t.Errorf("truth_basis = %v, want code_hint", got)
	}
	if got := hint["hint_type"]; got != "relationship" {
		t.Errorf("hint_type = %v, want relationship", got)
	}
	// The two fields that keep a hint from being read as a fact. A caller that
	// cannot see these has no way to know the hint is not corroborated truth.
	if got := hint["promotion_policy"]; got != facts.SemanticPromotionRequiresDeterministicEvidence {
		t.Errorf("promotion_policy = %v, want %q", got, facts.SemanticPromotionRequiresDeterministicEvidence)
	}
	if got := hint["corroboration_state"]; got != facts.SemanticCorroborationUncorroborated {
		t.Errorf("corroboration_state = %v, want %q", got, facts.SemanticCorroborationUncorroborated)
	}
}
