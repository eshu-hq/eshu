// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestCapture_TruthEnvelopeVerbatimAndExcerptDropped proves the captured
// query.TruthEnvelope is stored verbatim (not summarized or re-derived) and
// that citation excerpts (inline content bytes) never appear in the default
// bundle even when the caller supplies them via Citations.
func TestCapture_TruthEnvelopeVerbatimAndExcerptDropped(t *testing.T) {
	t.Parallel()

	truth := &query.TruthEnvelope{
		Level:      query.TruthLevelDerived,
		Capability: "trace.service_story",
		Profile:    query.ProfileLocalAuthoritative,
		Basis:      query.TruthBasisContentIndex,
		Backend:    query.GraphBackendNornicDB,
		Reason:     "derived from content index",
	}
	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
		Envelope: query.ResponseEnvelope{
			Data:  map[string]any{"owner": "platform-team"},
			Truth: truth,
		},
		Citations: []CitationRef{
			{Kind: "file", RepoID: "demo/service", RelativePath: "main.go", CitationID: "citation:abc"},
		},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}

	if bundle.Response.Truth == nil || *bundle.Response.Truth != *truth {
		t.Fatalf("Response.Truth = %+v, want verbatim %+v", bundle.Response.Truth, truth)
	}

	raw := mustMarshal(t, bundle)
	if strings.Contains(string(raw), "\"excerpt\"") {
		t.Fatalf("serialized bundle contains an excerpt field; CitationRef must never carry inline content bytes")
	}
}

// TestCapture_FactRefsDefaultToUnavailable proves that when a caller does not
// supply resolved fact references, Slice 1 records an explicit
// fact_refs_state of "unavailable" with a reason, rather than silently
// emitting an empty (and ambiguous) fact_refs list.
func TestCapture_FactRefsDefaultToUnavailable(t *testing.T) {
	t.Parallel()

	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
		Envelope: query.ResponseEnvelope{
			Data:  map[string]any{"owner": "platform-team"},
			Truth: &query.TruthEnvelope{Level: query.TruthLevelExact},
		},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	if bundle.Evidence.FactRefsState != "unavailable" {
		t.Fatalf("Evidence.FactRefsState = %q, want %q", bundle.Evidence.FactRefsState, "unavailable")
	}
	if strings.TrimSpace(bundle.Evidence.FactRefsReason) == "" {
		t.Fatalf("Evidence.FactRefsReason is empty, want an explicit reason")
	}
}

// TestCapture_NilResponseDataYieldsNullWithDigest proves the empty-state edge:
// a response envelope with nil Data must still produce a valid bundle whose
// Response.Data is the JSON null literal (redactValue(nil) -> nil ->
// json.Marshal -> "null") and whose DataDigest is a non-empty canonical
// digest of that null, not an error or an empty string.
func TestCapture_NilResponseDataYieldsNullWithDigest(t *testing.T) {
	t.Parallel()

	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
		Envelope: query.ResponseEnvelope{
			Data:  nil,
			Truth: &query.TruthEnvelope{Level: query.TruthLevelExact},
		},
	})
	if err != nil {
		t.Fatalf("Capture(nil Data) error = %v, want nil", err)
	}
	if got := string(bundle.Response.Data); got != "null" {
		t.Fatalf("Response.Data = %q, want %q for nil response data", got, "null")
	}
	if strings.TrimSpace(bundle.Response.DataDigest) == "" {
		t.Fatalf("Response.DataDigest is empty, want a canonical digest of the null value")
	}
	if err := Validate(bundle, ValidateOptions{RequirePublic: true}); err != nil {
		t.Fatalf("Validate(nil-data bundle, RequirePublic) error = %v, want nil", err)
	}
}

// A sensitive-shaped key inside an error envelope's Details must be REDACTED,
// not merely rejected. Before #5059, Response.Error was copied verbatim and the
// only thing standing between it and a shared bundle was the final Validate
// gate — so a user whose error happened to carry an api_key got no bundle at
// all, while every other field would have redacted and continued.
func TestCapture_ErrorDetailsAreRedacted(t *testing.T) {
	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
		Envelope: query.ResponseEnvelope{
			Data: map[string]any{"owner": "platform-team"},
			Error: &query.ErrorEnvelope{
				Code:    "internal",
				Message: "upstream refused the call",
				Details: map[string]any{
					"api_key":  "sk-live-do-not-share",
					"attempts": 3,
					"nested":   map[string]any{"password": "hunter2", "host": "svc"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v; a sensitive key in Details must redact and continue, not refuse", err)
	}

	got := bundle.Response.Error
	if got == nil {
		t.Fatal("Capture() dropped the error envelope entirely; Code and Message are not sensitive")
	}
	if got.Code != "internal" || got.Message != "upstream refused the call" {
		t.Errorf("non-payload error fields changed: code=%q message=%q", got.Code, got.Message)
	}
	if _, present := got.Details["api_key"]; present {
		t.Errorf("api_key survived redaction in Details: %#v", got.Details)
	}
	if got.Details["attempts"] != 3 {
		t.Errorf("benign Details key was dropped: %#v", got.Details)
	}
	nested, ok := got.Details["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested Details map missing or wrong type: %#v", got.Details)
	}
	if _, present := nested["password"]; present {
		t.Errorf("nested password survived redaction: %#v", nested)
	}
	if nested["host"] != "svc" {
		t.Errorf("benign nested key was dropped: %#v", nested)
	}

	// Assert the exact key names, not just that SOME rule was recorded. The
	// absence assertions above already prove the redaction happened; this is
	// about the bundle's own self-description, where recording the wrong name
	// (or none) is a metadata bug a "non-empty rule" check cannot see
	// (#5964 review). redactValue records the bare key, so these are the names.
	var sawAPIKey, sawPassword bool
	for _, rule := range bundle.Redaction.Rules {
		switch rule {
		case "api_key":
			sawAPIKey = true
		case "password":
			sawPassword = true
		}
	}
	if !sawAPIKey {
		t.Errorf("api_key not recorded in redaction rules: %v", bundle.Redaction.Rules)
	}
	if !sawPassword {
		t.Errorf("nested password not recorded in redaction rules: %v", bundle.Redaction.Rules)
	}
}

// The caller's envelope must not be mutated by capture — a CLI that captures
// and then prints the same envelope would otherwise show redacted output as if
// that were the real error.
func TestCapture_ErrorRedactionDoesNotMutateCallerEnvelope(t *testing.T) {
	details := map[string]any{"api_key": "sk-live-do-not-share", "attempts": 1}
	envelope := &query.ErrorEnvelope{Code: "internal", Message: "boom", Details: details}

	if _, err := Capture(CaptureInput{
		Surface:  "api",
		Target:   "/api/v0/services/checkout/story",
		Envelope: query.ResponseEnvelope{Data: map[string]any{"ok": true}, Error: envelope},
	}); err != nil {
		t.Fatalf("Capture() error = %v", err)
	}

	if _, present := details["api_key"]; !present {
		t.Error("Capture mutated the caller's Details map; it must redact a copy")
	}
}

// The other direction of the same aliasing hazard: the bundle must not share a
// Details map with the caller either. An empty-but-non-nil map is the case that
// slips through, because there is nothing in it to redact and the cheap thing to
// do is hand the same map back. A caller that fills that map in afterwards —
// error envelopes are often built incrementally — would then be writing straight
// into a bundle that has already passed its redaction check.
func TestCapture_EmptyErrorDetailsDoNotAliasTheCallersMap(t *testing.T) {
	details := map[string]any{}
	envelope := &query.ErrorEnvelope{Code: "internal", Message: "boom", Details: details}

	bundle, err := Capture(CaptureInput{
		Surface:  "api",
		Target:   "/api/v0/services/checkout/story",
		Envelope: query.ResponseEnvelope{Data: map[string]any{"ok": true}, Error: envelope},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}

	details["api_key"] = "sk-live-do-not-share"

	if _, present := bundle.Response.Error.Details["api_key"]; present {
		t.Error("the bundle's Details map aliases the caller's; a later write reached captured output unredacted")
	}
	if err := Validate(bundle, ValidateOptions{}); err != nil {
		t.Errorf("Validate() after the caller's write = %v, want nil", err)
	}
}

// Nil Details stay nil rather than becoming an empty object, so `details` keeps
// being omitted from the serialized error (the field is `json:",omitempty"`).
func TestCapture_NilErrorDetailsStayNil(t *testing.T) {
	envelope := &query.ErrorEnvelope{Code: "internal", Message: "boom"}

	bundle, err := Capture(CaptureInput{
		Surface:  "api",
		Target:   "/api/v0/services/checkout/story",
		Envelope: query.ResponseEnvelope{Data: map[string]any{"ok": true}, Error: envelope},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if bundle.Response.Error.Details != nil {
		t.Errorf("nil Details became %#v", bundle.Response.Error.Details)
	}
}

// A nil error envelope stays nil rather than becoming an empty object.
func TestCapture_NilErrorEnvelopeStaysNil(t *testing.T) {
	bundle, err := Capture(CaptureInput{
		Surface:  "api",
		Target:   "/api/v0/services/checkout/story",
		Envelope: query.ResponseEnvelope{Data: map[string]any{"ok": true}},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if bundle.Response.Error != nil {
		t.Errorf("nil error became %#v", bundle.Response.Error)
	}
}
