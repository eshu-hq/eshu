// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replay_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay"
)

// rawRecording is a deliberately "raw live output" document: object keys are
// unsorted, the volatile fields carry run-specific values, the facts arrive out
// of stable-fact-key order, the scopes arrive out of scope-id order, a secret
// hides inside a nested payload, and a numeric field uses a form that a float
// round-trip would churn. Canonicalization must tame all of it.
const rawRecording = `{
  "schema_version": "1",
  "collector": "kubernetes_live",
  "scopes": [
    {
      "source_system": "kubernetes_live",
      "scope_id": "kubernetes_live:cluster:zeta",
      "generation_id": "run-9f3c-zeta",
      "observed_at": "2026-06-25T18:42:11.523Z",
      "scope_kind": "cluster",
      "collector_kind": "kubernetes_live",
      "facts": [
        {
          "stable_fact_key": "k8s:zeta:b",
          "fact_kind": "kubernetes_live.pod_template",
          "schema_version": "1.0.0",
          "payload": {"replicas": 3, "name": "b"}
        },
        {
          "stable_fact_key": "k8s:zeta:a",
          "fact_kind": "kubernetes_live.pod_template",
          "schema_version": "1.0.0",
          "payload": {"name": "a", "api_token": "live-secret-zzz"}
        }
      ]
    },
    {
      "source_system": "kubernetes_live",
      "scope_id": "kubernetes_live:cluster:alpha",
      "generation_id": "run-1a2b-alpha",
      "observed_at": "2026-06-25T18:42:09.001Z",
      "scope_kind": "cluster",
      "collector_kind": "kubernetes_live",
      "facts": [
        {
          "stable_fact_key": "k8s:alpha:a",
          "fact_kind": "kubernetes_live.pod_template",
          "schema_version": "1.0.0",
          "payload": {"name": "a"}
        }
      ]
    }
  ]
}`

func canonicalOpts() replay.CanonicalOptions {
	return replay.DefaultCanonicalOptions().WithRedactedKeys("api_token")
}

// TestCanonicalizeIsIdempotent is the first-class acceptance case for R-1:
// record -> canonicalize twice -> byte-identical. The canonical form of a
// canonical form must equal the canonical form, or a recorder that re-runs
// would churn the whole fixture on every refresh.
func TestCanonicalizeIsIdempotent(t *testing.T) {
	t.Parallel()

	first, err := replay.Canonicalize([]byte(rawRecording), canonicalOpts())
	if err != nil {
		t.Fatalf("first Canonicalize: %v", err)
	}
	second, err := replay.Canonicalize(first, canonicalOpts())
	if err != nil {
		t.Fatalf("second Canonicalize: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonicalization is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestCanonicalizeSortsObjectKeys proves every object's keys are emitted in
// sorted order regardless of source order.
func TestCanonicalizeSortsObjectKeys(t *testing.T) {
	t.Parallel()

	out, err := replay.Canonicalize([]byte(rawRecording), canonicalOpts())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	got := string(out)
	// The top-level object must lead with "collector" before "schema_version"
	// before "scopes" — sorted, not the source order (schema_version first).
	collectorAt := strings.Index(got, `"collector"`)
	schemaAt := strings.Index(got, `"schema_version"`)
	scopesAt := strings.Index(got, `"scopes"`)
	if collectorAt < 0 || collectorAt >= schemaAt || schemaAt >= scopesAt {
		t.Fatalf("top-level keys not sorted (collector=%d schema_version=%d scopes=%d):\n%s",
			collectorAt, schemaAt, scopesAt, got)
	}
}

// TestCanonicalizeNormalizesVolatileFields proves observed_at collapses to its
// fixed sentinel, while generation_id is normalized to a deterministic per-scope
// value: stable across re-records, but UNIQUE per scope. A single fixed sentinel
// would collide multiple scopes on the generation_id primary key — this test
// guards that regression directly.
func TestCanonicalizeNormalizesVolatileFields(t *testing.T) {
	t.Parallel()

	out, err := replay.Canonicalize([]byte(rawRecording), canonicalOpts())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal canonical output: %v", err)
	}
	scopes, ok := doc["scopes"].([]any)
	if !ok || len(scopes) != 2 {
		t.Fatalf("scopes = %#v, want 2-element array", doc["scopes"])
	}
	seenGen := map[string]bool{}
	for i, raw := range scopes {
		sc := raw.(map[string]any)
		if got := sc["observed_at"]; got != replay.SentinelObservedAt {
			t.Errorf("scope[%d].observed_at = %v, want sentinel %q", i, got, replay.SentinelObservedAt)
		}
		gen, _ := sc["generation_id"].(string)
		if !strings.HasPrefix(gen, replay.GenerationIDPrefix+"-") {
			t.Errorf("scope[%d].generation_id = %q, want %q-prefixed derived value", i, gen, replay.GenerationIDPrefix)
		}
		// The original run-specific id must be gone.
		for _, raw := range []string{"run-9f3c-zeta", "run-1a2b-alpha"} {
			if gen == raw {
				t.Errorf("scope[%d].generation_id still carries run-specific value %q", i, raw)
			}
		}
		if seenGen[gen] {
			t.Errorf("scope[%d].generation_id %q collides with another scope (primary-key violation)", i, gen)
		}
		seenGen[gen] = true
	}
}

// TestDerivedGenerationIDMatchesCanonicalization proves the exported
// DerivedGenerationID returns exactly the generation_id Canonicalize derives for
// a scope from its scope_id, so a recorder that stamps it makes its run's
// generation_id already canonical (record is a no-op on the field). A drift
// between the two would silently rewrite a stamped generation_id on record.
func TestDerivedGenerationIDMatchesCanonicalization(t *testing.T) {
	t.Parallel()

	const scopeID = "kubernetes_live:cluster:zeta"
	want := replay.DerivedGenerationID(scopeID)
	if !strings.HasPrefix(want, replay.GenerationIDPrefix+"-") {
		t.Fatalf("DerivedGenerationID = %q, want %q-prefixed", want, replay.GenerationIDPrefix)
	}

	out, err := replay.Canonicalize([]byte(rawRecording), canonicalOpts())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	scopes := doc["scopes"].([]any)
	var got string
	for _, raw := range scopes {
		sc := raw.(map[string]any)
		if sc["scope_id"] == scopeID {
			got = sc["generation_id"].(string)
		}
	}
	if got != want {
		t.Errorf("canonicalized generation_id = %q, DerivedGenerationID = %q; must match", got, want)
	}
}

// TestCanonicalizeRejectsTrailingContent proves a valid first JSON value
// followed by a stray delimiter or second value is rejected, not silently
// normalized into a clean fixture (a corrupted recording must fail loudly).
func TestCanonicalizeRejectsTrailingContent(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`{"a":1}]`, `{"a":1} {"b":2}`, `{"a":1}}`, `[1,2] 3`} {
		if _, err := replay.Canonicalize([]byte(in), replay.DefaultCanonicalOptions()); err == nil {
			t.Errorf("Canonicalize(%q) accepted trailing content, want error", in)
		}
	}
}

// TestCanonicalizeOrdersScopesAndFacts proves scopes sort by scope_id and facts
// sort by stable_fact_key, independent of source order.
func TestCanonicalizeOrdersScopesAndFacts(t *testing.T) {
	t.Parallel()

	out, err := replay.Canonicalize([]byte(rawRecording), canonicalOpts())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	scopes := doc["scopes"].([]any)
	first := scopes[0].(map[string]any)["scope_id"]
	second := scopes[1].(map[string]any)["scope_id"]
	if first != "kubernetes_live:cluster:alpha" || second != "kubernetes_live:cluster:zeta" {
		t.Fatalf("scopes not ordered by scope_id: got [%v, %v]", first, second)
	}

	zeta := scopes[1].(map[string]any)
	facts := zeta["facts"].([]any)
	fk0 := facts[0].(map[string]any)["stable_fact_key"]
	fk1 := facts[1].(map[string]any)["stable_fact_key"]
	if fk0 != "k8s:zeta:a" || fk1 != "k8s:zeta:b" {
		t.Fatalf("facts not ordered by stable_fact_key: got [%v, %v]", fk0, fk1)
	}
}

// TestCanonicalizeRedactsConfiguredSecrets proves a configured secret key is
// redacted wherever it appears, including nested inside a fact payload.
func TestCanonicalizeRedactsConfiguredSecrets(t *testing.T) {
	t.Parallel()

	out, err := replay.Canonicalize([]byte(rawRecording), canonicalOpts())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "live-secret-zzz") {
		t.Fatalf("secret value leaked into canonical output:\n%s", got)
	}
	if !strings.Contains(got, replay.RedactedSentinel) {
		t.Fatalf("redaction sentinel %q absent from output:\n%s", replay.RedactedSentinel, got)
	}
}

// TestCanonicalizePreservesNumericFidelity proves integers stay integers — a
// naive float64 round-trip would rewrite 3 as 3 (fine) but could rewrite large
// or fractional values; json.Number preservation keeps the literal intact so
// re-records do not churn numbers.
func TestCanonicalizePreservesNumericFidelity(t *testing.T) {
	t.Parallel()

	in := `{"a":{"big":12345678901234567890,"frac":1.50,"n":3}}`
	out, err := replay.Canonicalize([]byte(in), replay.DefaultCanonicalOptions())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	got := string(out)
	for _, want := range []string{"12345678901234567890", "1.50", "3"} {
		if !strings.Contains(got, want) {
			t.Errorf("numeric literal %q not preserved in:\n%s", want, got)
		}
	}
}

// TestCanonicalizeRejectsInvalidJSON proves malformed input is a loud error,
// never a silent empty canonical form.
func TestCanonicalizeRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	if _, err := replay.Canonicalize([]byte("{not json"), replay.DefaultCanonicalOptions()); err == nil {
		t.Fatal("Canonicalize accepted malformed JSON, want error")
	}
}

// TestCanonicalizePreservesOpaquePayload proves volatile/derived normalization
// is confined to the cassette envelope: a fact payload that happens to carry
// observed_at or generation_id keeps the collector's real values verbatim
// (the recorder's verbatim-payload contract), while the scope-level observed_at
// and generation_id are still collapsed/derived. Secret redaction still reaches
// into the payload.
func TestCanonicalizePreservesOpaquePayload(t *testing.T) {
	doc := `{
  "schema_version": "1",
  "scopes": [
    {
      "scope_id": "s1",
      "observed_at": "2026-06-25T12:00:00Z",
      "generation_id": "gen-run-specific-123",
      "facts": [
        {
          "stable_fact_key": "k1",
          "payload": {
            "observed_at": "2026-06-25T09:30:00Z",
            "generation_id": "deployment-gen-42",
            "api_token": "SECRET"
          }
        }
      ]
    }
  ]
}`
	opts := replay.DefaultCanonicalOptions().WithRedactedKeys("api_token")
	out, err := replay.Canonicalize([]byte(doc), opts)
	if err != nil {
		t.Fatalf("Canonicalize() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal canonical: %v", err)
	}
	scope := got["scopes"].([]any)[0].(map[string]any)
	// Scope-level volatile/derived ARE normalized.
	if scope["observed_at"] != replay.SentinelObservedAt {
		t.Errorf("scope observed_at = %v, want sentinel %q", scope["observed_at"], replay.SentinelObservedAt)
	}
	if gen, _ := scope["generation_id"].(string); !strings.HasPrefix(gen, replay.GenerationIDPrefix) {
		t.Errorf("scope generation_id = %v, want derived (prefix %q)", scope["generation_id"], replay.GenerationIDPrefix)
	}
	// Payload-level keys of the same name are preserved VERBATIM.
	payload := scope["facts"].([]any)[0].(map[string]any)["payload"].(map[string]any)
	if payload["observed_at"] != "2026-06-25T09:30:00Z" {
		t.Errorf("payload observed_at = %v, want verbatim 2026-06-25T09:30:00Z", payload["observed_at"])
	}
	if payload["generation_id"] != "deployment-gen-42" {
		t.Errorf("payload generation_id = %v, want verbatim deployment-gen-42", payload["generation_id"])
	}
	// Redaction still reaches into the payload.
	if payload["api_token"] != replay.RedactedSentinel {
		t.Errorf("payload api_token = %v, want redacted %q", payload["api_token"], replay.RedactedSentinel)
	}
}
