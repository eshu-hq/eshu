// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("azure-tag-observation-test-key-material"))
	if err != nil {
		t.Fatalf("new redaction key: %v", err)
	}
	return key
}

// TestNewTagObservationEnvelopeFingerprintsValues proves the tag-evidence fact
// carries deterministic, key-derived value fingerprints (never raw tag value
// text) while preserving the tag keys as correlation taxonomy.
func TestNewTagObservationEnvelopeFingerprintsValues(t *testing.T) {
	obs := testResourceObservation(t)
	obs.Tags = map[string]string{"env": "prod", "owner": "payments-team"}
	key := testRedactionKey(t)

	env, err := NewTagObservationEnvelope(obs, key)
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope error: %v", err)
	}
	if env.FactKind != facts.AzureTagObservationFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.AzureTagObservationFactKind)
	}
	if env.SchemaVersion != facts.AzureTagObservationSchemaVersion {
		t.Fatalf("SchemaVersion = %q", env.SchemaVersion)
	}
	if env.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", env.CollectorKind, CollectorKind)
	}
	if env.ScopeID != obs.Boundary.ScopeID || env.GenerationID != obs.Boundary.GenerationID {
		t.Fatalf("scope/generation not propagated: %q %q", env.ScopeID, env.GenerationID)
	}
	if env.Payload["arm_resource_id"] != obs.ARMResourceID {
		t.Fatalf("arm_resource_id = %#v", env.Payload["arm_resource_id"])
	}
	if env.Payload["redaction_policy_version"] != RedactionPolicyVersion {
		t.Fatalf("redaction_policy_version = %#v", env.Payload["redaction_policy_version"])
	}

	fps, ok := env.Payload["tag_value_fingerprints"].(map[string]string)
	if !ok {
		t.Fatalf("tag_value_fingerprints type = %T", env.Payload["tag_value_fingerprints"])
	}
	if len(fps) != 2 {
		t.Fatalf("fingerprint count = %d, want 2", len(fps))
	}
	for k, marker := range fps {
		if marker == "prod" || marker == "payments-team" {
			t.Fatalf("raw tag value leaked for key %q: %q", k, marker)
		}
		if strings.TrimSpace(marker) == "" {
			t.Fatalf("empty fingerprint for key %q", k)
		}
	}

	// Determinism for the same key; dependence on the key material.
	envSame, err := NewTagObservationEnvelope(obs, key)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if envSame.Payload["tag_value_fingerprints"].(map[string]string)["env"] != fps["env"] {
		t.Fatal("fingerprint not deterministic for the same key")
	}
	otherKey, err := redact.NewKey([]byte("a-different-azure-tag-key-entirely"))
	if err != nil {
		t.Fatalf("other key: %v", err)
	}
	envOther, err := NewTagObservationEnvelope(obs, otherKey)
	if err != nil {
		t.Fatalf("other-key build: %v", err)
	}
	if envOther.Payload["tag_value_fingerprints"].(map[string]string)["env"] == fps["env"] {
		t.Fatal("fingerprint must depend on the redaction key")
	}

	keys, ok := env.Payload["tag_keys"].([]string)
	if !ok || len(keys) != 2 || keys[0] != "env" || keys[1] != "owner" {
		t.Fatalf("tag_keys = %#v, want sorted [env owner]", env.Payload["tag_keys"])
	}
	if env.Payload["tag_count"] != 2 {
		t.Fatalf("tag_count = %#v, want 2", env.Payload["tag_count"])
	}
	if env.Payload["tag_truncated"] != false {
		t.Fatalf("tag_truncated = %#v, want false", env.Payload["tag_truncated"])
	}
}

// TestNewTagObservationEnvelopeStableKeyIgnoresTagValueChurn proves the stable
// fact key is identity-derived, so a changed tag value re-emits the same fact
// row within a generation instead of splitting it.
func TestNewTagObservationEnvelopeStableKeyIgnoresTagValueChurn(t *testing.T) {
	key := testRedactionKey(t)
	a := testResourceObservation(t)
	a.Tags = map[string]string{"env": "prod"}
	b := testResourceObservation(t)
	b.Tags = map[string]string{"env": "staging"}

	ea, err := NewTagObservationEnvelope(a, key)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	eb, err := NewTagObservationEnvelope(b, key)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if ea.StableFactKey != eb.StableFactKey {
		t.Fatalf("tag value churn split stable key: %q vs %q", ea.StableFactKey, eb.StableFactKey)
	}
	if ea.Payload["tag_value_fingerprints"].(map[string]string)["env"] ==
		eb.Payload["tag_value_fingerprints"].(map[string]string)["env"] {
		t.Fatal("different tag values must fingerprint differently")
	}
}

// TestNewTagObservationEnvelopeRejectsEmptyAndKeyless proves untagged resources
// and a zero redaction key fail closed: an empty tag observation is missing
// evidence, not a clean match, and fingerprinting must never run without a key.
func TestNewTagObservationEnvelopeRejectsEmptyAndKeyless(t *testing.T) {
	key := testRedactionKey(t)

	noTags := testResourceObservation(t)
	noTags.Tags = nil
	if _, err := NewTagObservationEnvelope(noTags, key); err == nil {
		t.Fatal("expected error for observation with no tags")
	}

	blank := testResourceObservation(t)
	blank.Tags = map[string]string{"   ": "x"}
	if _, err := NewTagObservationEnvelope(blank, key); err == nil {
		t.Fatal("expected error when all tag keys are blank")
	}

	withTags := testResourceObservation(t)
	withTags.Tags = map[string]string{"env": "prod"}
	if _, err := NewTagObservationEnvelope(withTags, redact.Key{}); err == nil {
		t.Fatal("expected error for a zero redaction key")
	}
}

// TestFingerprintTagValuesBoundsCardinality proves the helper deterministically
// caps the number of tags per observation so a pathological resource cannot emit
// an unbounded payload.
func TestFingerprintTagValuesBoundsCardinality(t *testing.T) {
	key := testRedactionKey(t)
	tags := make(map[string]string, maxTagObservationEntries+10)
	for i := 0; i < maxTagObservationEntries+10; i++ {
		tags[fmt.Sprintf("k%03d", i)] = "v"
	}
	out, keys, truncated := FingerprintTagValues(tags, key)
	if !truncated {
		t.Fatal("expected truncation for over-cardinality tags")
	}
	if len(out) != maxTagObservationEntries || len(keys) != maxTagObservationEntries {
		t.Fatalf("retained out=%d keys=%d, want %d", len(out), len(keys), maxTagObservationEntries)
	}
	if keys[0] != "k000" {
		t.Fatalf("truncation not deterministic by sorted key: first=%q", keys[0])
	}
}
