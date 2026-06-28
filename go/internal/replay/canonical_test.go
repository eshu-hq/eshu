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

// TestCanonicalizeNormalizesVolatileFields proves observed_at and generation_id
// collapse to their fixed sentinels so a re-record does not churn them.
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
	for i, raw := range scopes {
		sc := raw.(map[string]any)
		if got := sc["observed_at"]; got != replay.SentinelObservedAt {
			t.Errorf("scope[%d].observed_at = %v, want sentinel %q", i, got, replay.SentinelObservedAt)
		}
		if got := sc["generation_id"]; got != replay.SentinelGenerationID {
			t.Errorf("scope[%d].generation_id = %v, want sentinel %q", i, got, replay.SentinelGenerationID)
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
