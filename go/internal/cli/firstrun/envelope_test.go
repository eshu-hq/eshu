// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
)

// TestParseEnvelopeRoundTrips proves the parser reads the canonical envelope
// emitted by `eshu first-run --json` (top-level data/truth/error).
func TestParseEnvelopeRoundTrips(t *testing.T) {
	raw := `{
  "data": {
    "command": "first-run",
    "runtime_shape": "local_binaries",
    "service_url": "http://localhost:8080",
    "repo_indexed": "complete",
    "repo_target": "/ws/demo",
    "readiness": "indexing complete",
    "query_answered": true,
    "query_summary": "repositories query returned 1 (e.g. demo)",
    "steps": []
  },
  "truth": {"level": "runtime", "completeness": "complete", "freshness": "current"},
  "error": null
}`
	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("ParseEnvelope error: %v", err)
	}
	if !env.Data.QueryAnswered {
		t.Fatal("Data.QueryAnswered = false, want true")
	}
	if env.Truth["completeness"] != "complete" {
		t.Fatalf("Truth completeness = %v, want complete", env.Truth["completeness"])
	}
	if env.Error != nil {
		t.Fatalf("Error = %v, want nil", env.Error)
	}
}

// TestParseEnvelopeCapturesError proves an error envelope round-trips so a
// consumer can reject failed runs.
func TestParseEnvelopeCapturesError(t *testing.T) {
	raw := `{"data":{"command":"first-run","query_answered":false},"truth":{"completeness":"partial"},"error":{"message":"verify runtime: no reachable API"}}`
	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("ParseEnvelope error: %v", err)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "no reachable API") {
		t.Fatalf("Error = %v, want message containing 'no reachable API'", env.Error)
	}
}

// TestParseEnvelopeRejectsMalformedPayload proves malformed input fails loudly
// instead of being silently consumed, including type mismatches nested inside
// blocks a consumer never reads (steps, diagnosis). The decode must stay as
// strict as the canonical first-run envelope so a corrupt artifact cannot pass
// the benchmark or the evidence report by accident.
func TestParseEnvelopeRejectsMalformedPayload(t *testing.T) {
	cases := map[string]string{
		"not json":           `{"data":`,
		"steps wrong type":   `{"data":{"command":"first-run","steps":"broken"},"truth":{},"error":null}`,
		"diagnosis mistyped": `{"data":{"command":"first-run","diagnosis":{"class":5}},"truth":{},"error":null}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseEnvelope([]byte(raw)); err == nil {
				t.Fatalf("ParseEnvelope(%s) error = nil, want decode failure", name)
			}
		})
	}
}

// TestParseEnvelopePreservesDiagnosisCause pins the round trip that the
// evidence report depends on. Diagnostic.MarshalJSON emits the preserved root
// cause under "cause" because Underlying is unexported and tagged `json:"-"`,
// so a plain struct decode reads every other field and silently drops that one.
//
// The consumer is real rather than hypothetical: `eshu first-run report --from
// <saved envelope>` decodes through ParseEnvelope, and evidence_render.go
// prints the "cause" line only when rootCause() is non-empty. Without a
// matching UnmarshalJSON the operator gets a report whose root cause is blank,
// which contradicts this type's own doc comment ("never discarded") and
// AGENTS.md ("Do not swallow it").
//
// The fixture populates Underlying deliberately. A zero-valued Diagnostic would
// round-trip an empty cause and pass whether or not the decoder exists, which
// is the vacuous version of this test.
func TestParseEnvelopePreservesDiagnosisCause(t *testing.T) {
	const sentinel = "bootstrap mount /repos not visible inside container"

	original := Envelope{
		Data: Result{
			Diagnostic: &Diagnostic{
				Class:         FailureClass("bootstrap_mount_missing"),
				Summary:       "the repo mount is not visible to the container",
				RecoverySteps: []string{"check the compose bind mount"},
				DocsLink:      "docs/public/run-locally/docker-compose.md",
				Underlying:    errors.New(sentinel),
			},
		},
		Truth: map[string]any{"backend": "nornicdb"},
	}

	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	decoded, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}
	if decoded.Data.Diagnostic == nil {
		t.Fatal("decoded diagnostic is nil; the fixture set one, so the decode dropped the whole block")
	}
	if got := decoded.Data.Diagnostic.rootCause(); got != sentinel {
		t.Fatalf("rootCause after round trip = %q, want %q; the cause key is emitted by MarshalJSON and must decode back", got, sentinel)
	}

	// The emitted key set must survive the round trip too, so a future field
	// cannot be added on one side only.
	reMarshalled, err := json.Marshal(decoded.Data.Diagnostic)
	if err != nil {
		t.Fatalf("re-marshal diagnostic: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(reMarshalled, &keys); err != nil {
		t.Fatalf("decode re-marshalled keys: %v", err)
	}
	got := make([]string, 0, len(keys))
	for k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"cause", "class", "docs_link", "recovery_steps", "summary"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("re-marshalled keys = %v, want exactly %v", got, want)
	}
}
