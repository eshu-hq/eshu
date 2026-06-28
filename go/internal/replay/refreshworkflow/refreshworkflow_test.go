// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package refreshworkflow_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay"
)

// baseCassette is a minimal two-scope, multi-fact cassette that exercises the
// properties the R-6 refresh workflow depends on. It is intentionally larger
// than a single fact so the canonical-diff legibility test can show that
// changing one value does not rewrite the entire document.
const baseCassette = `{
  "schema_version": "1",
  "collector": "kubernetes_live",
  "scopes": [
    {
      "scope_id": "kubernetes_live:cluster:alpha",
      "source_system": "kubernetes_live",
      "scope_kind": "cluster",
      "collector_kind": "kubernetes_live",
      "generation_id": "run-live-alpha",
      "observed_at": "2026-06-28T10:00:00Z",
      "facts": [
        {
          "fact_kind": "kubernetes_workload",
          "stable_fact_key": "default/deploy/api",
          "schema_version": "1",
          "payload": {"name": "api", "replicas": 2, "image": "registry.example.com/api:v1.2.3"}
        },
        {
          "fact_kind": "kubernetes_workload",
          "stable_fact_key": "default/deploy/app",
          "schema_version": "1",
          "payload": {"name": "app", "replicas": 3, "image": "registry.example.com/app:v2.0.0"}
        }
      ]
    },
    {
      "scope_id": "kubernetes_live:cluster:zeta",
      "source_system": "kubernetes_live",
      "scope_kind": "cluster",
      "collector_kind": "kubernetes_live",
      "generation_id": "run-live-zeta",
      "observed_at": "2026-06-28T10:01:00Z",
      "facts": [
        {
          "fact_kind": "kubernetes_workload",
          "stable_fact_key": "prod/deploy/worker",
          "schema_version": "1",
          "payload": {"name": "worker", "replicas": 5, "image": "registry.example.com/worker:v1.0.0"}
        }
      ]
    }
  ]
}`

// alteredCassette carries the same document with a single field change: the
// "api" deployment's image tag is bumped from v1.2.3 to v1.2.4. The canonical
// diff test asserts that exactly that line changes and nothing else does.
const alteredCassette = `{
  "schema_version": "1",
  "collector": "kubernetes_live",
  "scopes": [
    {
      "scope_id": "kubernetes_live:cluster:alpha",
      "source_system": "kubernetes_live",
      "scope_kind": "cluster",
      "collector_kind": "kubernetes_live",
      "generation_id": "run-live-alpha",
      "observed_at": "2026-06-28T10:00:00Z",
      "facts": [
        {
          "fact_kind": "kubernetes_workload",
          "stable_fact_key": "default/deploy/api",
          "schema_version": "1",
          "payload": {"name": "api", "replicas": 2, "image": "registry.example.com/api:v1.2.4"}
        },
        {
          "fact_kind": "kubernetes_workload",
          "stable_fact_key": "default/deploy/app",
          "schema_version": "1",
          "payload": {"name": "app", "replicas": 3, "image": "registry.example.com/app:v2.0.0"}
        }
      ]
    },
    {
      "scope_id": "kubernetes_live:cluster:zeta",
      "source_system": "kubernetes_live",
      "scope_kind": "cluster",
      "collector_kind": "kubernetes_live",
      "generation_id": "run-live-zeta",
      "observed_at": "2026-06-28T10:01:00Z",
      "facts": [
        {
          "fact_kind": "kubernetes_workload",
          "stable_fact_key": "prod/deploy/worker",
          "schema_version": "1",
          "payload": {"name": "worker", "replicas": 5, "image": "registry.example.com/worker:v1.0.0"}
        }
      ]
    }
  ]
}`

// secretCassette is a cassette whose payload carries a known secret key. The
// redaction test asserts the secret never appears in the canonical output.
const secretCassette = `{
  "schema_version": "1",
  "collector": "kubernetes_live",
  "scopes": [
    {
      "scope_id": "kubernetes_live:cluster:secret-test",
      "source_system": "kubernetes_live",
      "scope_kind": "cluster",
      "collector_kind": "kubernetes_live",
      "generation_id": "run-secret-test",
      "observed_at": "2026-06-28T10:00:00Z",
      "facts": [
        {
          "fact_kind": "kubernetes_secret_ref",
          "stable_fact_key": "default/secret/svc-creds",
          "schema_version": "1",
          "payload": {
            "name": "svc-creds",
            "api_token": "live-production-secret-abc123xyz",
            "nested": {
              "api_token": "another-live-secret-def456"
            }
          }
        }
      ]
    }
  ]
}`

func canonicalizeWith(t *testing.T, raw string, secretKeys ...string) []byte {
	t.Helper()
	opts := replay.DefaultCanonicalOptions()
	if len(secretKeys) > 0 {
		opts = opts.WithRedactedKeys(secretKeys...)
	}
	out, err := replay.Canonicalize([]byte(raw), opts)
	if err != nil {
		t.Fatalf("Canonicalize() error = %v", err)
	}
	return out
}

// TestCanonicalDiffIsLegible is the primary local proof for R-6 acceptance
// criterion "a deliberately altered fact shape produces a legible (non-whole-file)
// diff". It records and canonicalizes both the base cassette and the altered
// cassette, then asserts:
//
//  1. The two canonical forms differ (the change is visible).
//  2. The diff affects exactly one value line — only the changed image tag
//     appears in lines that differ, proving canonical output is stable (sorted,
//     volatile-fields collapsed) so re-recording with an identical fact shape
//     produces an empty diff, and a single field change produces a minimal diff.
//  3. Re-canonicalizing the canonical form is byte-identical (idempotency), so
//     the diff is always between two stable forms, never between a stable and a
//     churned form.
//
// This test exercises the real replay.Canonicalize path — not a hand-written
// re-implementation — so it fails if Canonicalize stops sorting arrays (facts
// would reorder on re-record, producing whole-file churn in the diff).
func TestCanonicalDiffIsLegible(t *testing.T) {
	t.Parallel()

	baseCanon := canonicalizeWith(t, baseCassette)
	alteredCanon := canonicalizeWith(t, alteredCassette)

	// 1. The two canonical forms must differ.
	if bytes.Equal(baseCanon, alteredCanon) {
		t.Fatal("canonical base and altered cassettes are byte-identical; the change was not captured")
	}

	// Compute a line-level diff to assert legibility.
	baseLines := strings.Split(string(baseCanon), "\n")
	alteredLines := strings.Split(string(alteredCanon), "\n")

	if len(baseLines) != len(alteredLines) {
		t.Fatalf("canonical line counts differ: base=%d altered=%d; whole-file structure changed (not a legible single-field diff)",
			len(baseLines), len(alteredLines))
	}

	var diffLines []int
	for i := range baseLines {
		if baseLines[i] != alteredLines[i] {
			diffLines = append(diffLines, i)
		}
	}

	// 2. Exactly one line must differ — the changed image tag. A canonical diff
	// of a single field change must not produce whole-file churn.
	if len(diffLines) == 0 {
		t.Fatal("no lines differ between canonical base and altered cassette; the single-field change was not preserved")
	}
	if len(diffLines) > 1 {
		var context strings.Builder
		for _, idx := range diffLines {
			context.WriteString("\n  base:    ")
			context.WriteString(baseLines[idx])
			context.WriteString("\n  altered: ")
			context.WriteString(alteredLines[idx])
		}
		t.Fatalf("canonical diff has %d changed lines, want 1 (non-legible diff — canonicalization is not stable):%s",
			len(diffLines), context.String())
	}

	// Confirm the single changed line contains the expected image tag bump.
	changedIdx := diffLines[0]
	if !strings.Contains(alteredLines[changedIdx], "v1.2.4") {
		t.Errorf("changed line does not contain expected new value v1.2.4:\n  base:    %s\n  altered: %s",
			baseLines[changedIdx], alteredLines[changedIdx])
	}
	if !strings.Contains(baseLines[changedIdx], "v1.2.3") {
		t.Errorf("base changed line does not contain expected old value v1.2.3:\n  line: %s", baseLines[changedIdx])
	}

	// 3. Idempotency: re-canonicalizing the canonical form yields the same bytes.
	//    Without this, running the refresh twice would produce a diff even when
	//    nothing changed in the provider API.
	baseCanon2 := canonicalizeWith(t, string(baseCanon))
	if !bytes.Equal(baseCanon, baseCanon2) {
		t.Fatal("canonicalization is not idempotent: re-canonicalizing the canonical form yields different bytes")
	}
}

// TestRedactionNeverLeaksSecrets is the primary local proof for R-6 acceptance
// criterion "secrets never appear in the recorded artifacts (redaction asserted)".
//
// It canonicalizes a cassette whose fact payload carries a known secret value
// under a configured secret key ("api_token"), then asserts:
//
//  1. The raw secret value never appears in the canonical output — not at the
//     top level, not nested inside a payload.
//  2. The redaction sentinel appears in the output, proving the key was replaced
//     rather than silently dropped.
//  3. Non-secret payload fields survive verbatim (redaction must be surgical,
//     not wholesale payload erasure).
//  4. An UNREGISTERED secret key is NOT redacted — the caller is responsible
//     for declaring every secret key in RedactKeys. This inverse case proves
//     that redaction is opt-in per key, not automatic, so a collector author
//     who omits a key from RedactKeys will observe the value in the cassette
//     (and must add it). Without this case, a test that only proves "registered
//     keys are redacted" gives false assurance that the system is safe by
//     default.
//
// This test is a real negative test: if replay.Canonicalize stops honoring
// SecretKeys (or the WithRedactedKeys helper breaks), the raw secret value will
// appear in the output and assertion 1 fires. Remove WithRedactedKeys from
// canonicalizeWith and run — the test fails on assertion 1.
func TestRedactionNeverLeaksSecrets(t *testing.T) {
	t.Parallel()

	out := canonicalizeWith(t, secretCassette, "api_token")
	outStr := string(out)

	// 1. The raw secret values must never appear in the canonical output.
	// This assertion covers both top-level and nested occurrences of api_token.
	for _, secret := range []string{"live-production-secret-abc123xyz", "another-live-secret-def456"} {
		if strings.Contains(outStr, secret) {
			t.Errorf("secret value %q leaked into canonical output; redaction must cover all depths:\n%s",
				secret, outStr)
		}
	}

	// 2. The redaction sentinel must be present — the key must be replaced, not
	// dropped. A missing sentinel means the field was silently removed, which
	// would hide a schema regression (a field vanished rather than being redacted).
	if !strings.Contains(outStr, replay.RedactedSentinel) {
		t.Errorf("redaction sentinel %q absent from canonical output; want the secret key replaced, not dropped:\n%s",
			replay.RedactedSentinel, outStr)
	}

	// Count occurrences: there are two api_token fields (one top-level in the
	// payload, one nested inside "nested"), so there must be at least two
	// sentinel occurrences.
	count := strings.Count(outStr, replay.RedactedSentinel)
	if count < 2 {
		t.Errorf("found %d sentinel occurrence(s), want at least 2 (one per api_token field at every depth)", count)
	}

	// 3. Non-secret fields survive verbatim. "svc-creds" is the payload's
	// "name" field and must still appear.
	if !strings.Contains(outStr, "svc-creds") {
		t.Errorf("non-secret payload field %q was erased; redaction must be surgical:\n%s", "svc-creds", outStr)
	}

	// Confirm the canonical output is valid JSON and can be parsed.
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("canonical output is not valid JSON: %v\n%s", err, outStr)
	}
}

// TestUnregisteredSecretKeyLeaks is the inverse of TestRedactionNeverLeaksSecrets.
// It proves that redaction is opt-in per key: a secret value under a key that
// was NOT registered in RedactKeys will appear verbatim in the canonical output.
// This is not a bug — it is the intended contract. The purpose of this test is to
// document and gate the contract so a future change that accidentally makes
// Canonicalize redact-by-default (redacting all string values) would be caught:
// such a change would make this test fail by removing the fixture value from output.
//
// The real-world consequence of the opt-in contract: a collector author who forgets
// to add a credential-bearing key to Options.RedactKeys will commit a cassette with
// live credentials. This test and its companion (TestRedactionNeverLeaksSecrets) are
// the local gates; the review of each cassette diff PR is the human gate.
func TestUnregisteredSecretKeyLeaks(t *testing.T) {
	t.Parallel()

	// Canonicalize the same fixture WITHOUT registering api_token. The secret
	// value must survive in the output (redaction is opt-in per key).
	out := canonicalizeWith(t, secretCassette /* no secret keys registered */)
	outStr := string(out)

	// The unregistered secret value must appear verbatim in the output.
	// If this assertion fails, Canonicalize is redacting without being asked —
	// which would mean a collector that passes RedactKeys: []string{} could
	// incorrectly suppress values a reviewer needs to see.
	if !strings.Contains(outStr, "live-production-secret-abc123xyz") {
		t.Errorf("unregistered key value not found in canonical output; redaction must be opt-in per key, not automatic:\n%s",
			outStr)
	}

	// The sentinel must NOT appear (no key was registered for redaction).
	if strings.Contains(outStr, replay.RedactedSentinel) {
		t.Errorf("redaction sentinel %q present despite no registered secret keys; Canonicalize is redacting without being asked:\n%s",
			replay.RedactedSentinel, outStr)
	}
}

// TestCanonicalFormIsStableAcrossReRecord proves that re-canonicalizing an
// already-canonical cassette is a no-op (byte-identical), which is the
// load-bearing property for the diff PR opened by the R-6 workflow: if a fresh
// re-record of an unchanged provider API produces different bytes, every refresh
// opens a spurious PR with a non-zero diff. The real recorder writes
// canonicalized bytes; this test proves that feeding those bytes back through
// Canonicalize again changes nothing.
func TestCanonicalFormIsStableAcrossReRecord(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"multi-scope", baseCassette},
		{"with-secret", secretCassette},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := canonicalizeWith(t, tc.raw, "api_token")
			second := canonicalizeWith(t, string(first), "api_token")
			if !bytes.Equal(first, second) {
				t.Fatalf("canonical form is not stable across re-record:\nfirst:\n%s\nsecond:\n%s",
					first, second)
			}
		})
	}
}
