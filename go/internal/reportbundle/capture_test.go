// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/sdk/go/collector"
)

// canarySentinels are unique, greppable sentinel VALUES planted under
// sensitive-shaped key names across query params, response data, and (for the
// private-triage case) fact payloads. Redaction is key-name based, so each
// sentinel must sit directly under a key collector.IsSensitiveKeyName flags;
// a sentinel value under a benign key would not be redacted and is not what
// this canary is testing.
const (
	sentinelParamValue          = "CANARY-PARAM-SENTINEL-7f3a9c"
	sentinelDataValue           = "CANARY-DATA-SENTINEL-2b81de"
	sentinelNestedValue         = "CANARY-NESTED-SENTINEL-e4419a"
	sentinelFactValue           = "CANARY-FACT-SENTINEL-91c0aa"
	sentinelExcerptMarker       = "CANARY-EXCERPT-SENTINEL-55d201"
	sentinelEmbeddedExcerptData = "CANARY-EMBEDDED-EXCERPT-SENTINEL-c02f18"
)

func canaryCaptureInput(includePayloads bool) CaptureInput {
	input := CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
		Method:  "GET",
		Params: map[string]any{
			"repo":    "demo/service",
			"api_key": sentinelParamValue,
		},
		Envelope: query.ResponseEnvelope{
			Data: map[string]any{
				"owner": "platform-team",
				"auth": map[string]any{
					"password": sentinelDataValue,
				},
				"rows": []any{
					map[string]any{"client_secret": sentinelNestedValue, "id": "row-1"},
				},
				"citations": []any{
					map[string]any{"repo_id": "demo/service", "excerpt": sentinelEmbeddedExcerptData},
				},
			},
			Truth: &query.TruthEnvelope{
				Level: query.TruthLevelExact,
				Basis: query.TruthBasisAuthoritativeGraph,
			},
		},
		ReporterNote:    "expected the owning team, got an empty list",
		IncludePayloads: includePayloads,
	}
	if includePayloads {
		input.PayloadExcerpts = []CitationExcerpt{
			{
				CitationRef: CitationRef{Kind: "file", RepoID: "demo/service", RelativePath: "main.go"},
				Excerpt:     sentinelExcerptMarker,
			},
		}
		input.PayloadFacts = []facts.Envelope{
			{
				FactID:        "fact-1",
				FactKind:      "repository",
				StableFactKey: "repo:demo/service",
				ScopeID:       "scope-1",
				GenerationID:  "gen-1",
				Payload:       map[string]any{"description": sentinelFactValue},
			},
		}
	}
	return input
}

// TestCapture_RedactionCanary is the acceptance-criterion test: it plants
// sensitive-shaped key names with unique sentinel VALUES in query params,
// response data (including nested objects and arrays), and fact payloads, then
// proves the default (public) bundle's serialized BYTES contain none of the
// sentinel values — not merely that the keys were renamed. This is stricter
// than a key-based check: a redactor that renamed keys but left values in
// place, or missed a nesting level, would leak the sentinel and fail here even
// though a key-name-only assertion would pass.
func TestCapture_RedactionCanary(t *testing.T) {
	t.Parallel()

	t.Run("default capture is public and drops all sentinel values", func(t *testing.T) {
		t.Parallel()

		bundle, err := Capture(canaryCaptureInput(false))
		if err != nil {
			t.Fatalf("Capture() error = %v, want nil", err)
		}

		if bundle.Redaction.Profile != ProfilePublic {
			t.Fatalf("Redaction.Profile = %q, want %q", bundle.Redaction.Profile, ProfilePublic)
		}
		if bundle.Payloads != nil {
			t.Fatalf("Payloads = %+v, want nil without --include-payloads", bundle.Payloads)
		}

		// (a) ValidateShareSafeKeys passes on the serialized default bundle.
		raw := mustMarshal(t, bundle)
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshal serialized bundle: %v", err)
		}
		if err := collector.ValidateShareSafeKeys(doc); err != nil {
			t.Fatalf("ValidateShareSafeKeys(serialized public bundle) error = %v, want nil", err)
		}
		if err := Validate(bundle, ValidateOptions{}); err != nil {
			t.Fatalf("Validate(public bundle) error = %v, want nil", err)
		}

		// (b) THE TEETH: no sentinel value appears as a substring anywhere in
		// the serialized bundle bytes. This catches a redactor that renames
		// keys but leaks values, which (a) alone would miss.
		text := string(raw)
		for _, sentinel := range []string{sentinelParamValue, sentinelDataValue, sentinelNestedValue, sentinelEmbeddedExcerptData} {
			if strings.Contains(text, sentinel) {
				t.Fatalf("serialized public bundle leaks sentinel value %q:\n%s", sentinel, text)
			}
		}
		// Check for the KEY form "excerpt": specifically, not the bare quoted
		// word — "excerpt" legitimately appears as a plain array element
		// inside Redaction.Rules, documenting that it was stripped.
		if strings.Contains(text, "\"excerpt\":") {
			t.Fatalf("serialized public bundle carries a live excerpt key embedded in response data; inline content bytes must be stripped from Data too, not only from the typed CitationRef")
		}

		// (c) fact payload bytes are absent entirely (refs only), so the
		// fact-only sentinel cannot appear even though this variant did not
		// plant one (guards against a future capture path inlining facts by
		// default).
		if strings.Contains(text, "payload") && strings.Contains(text, sentinelFactValue) {
			t.Fatalf("serialized public bundle unexpectedly carries fact payload sentinel")
		}
	})

	t.Run("include-payloads flips profile and exposes sentinels under private-triage", func(t *testing.T) {
		t.Parallel()

		bundle, err := Capture(canaryCaptureInput(true))
		if err != nil {
			t.Fatalf("Capture() error = %v, want nil", err)
		}

		// (d) profile flips, sentinels DO appear in the payload attachment,
		// --require-public fails it.
		if bundle.Redaction.Profile != ProfilePrivateTriage {
			t.Fatalf("Redaction.Profile = %q, want %q", bundle.Redaction.Profile, ProfilePrivateTriage)
		}
		if bundle.Payloads == nil {
			t.Fatalf("Payloads = nil, want a populated attachment under --include-payloads")
		}
		if strings.TrimSpace(bundle.Payloads.Warning) == "" {
			t.Fatalf("Payloads.Warning is empty, want a loud private-triage-only sentence")
		}

		raw := mustMarshal(t, bundle)
		text := string(raw)
		if !strings.Contains(text, sentinelExcerptMarker) {
			t.Fatalf("private-triage bundle does not carry the excerpt sentinel; --include-payloads should attach raw excerpts verbatim")
		}
		if !strings.Contains(text, sentinelFactValue) {
			t.Fatalf("private-triage bundle does not carry the fact payload sentinel; --include-payloads should attach fact payloads verbatim")
		}

		// The rest of the bundle (params/response data) is STILL redacted even
		// under --include-payloads: only the explicit payload attachment is
		// exempt.
		for _, sentinel := range []string{sentinelParamValue, sentinelDataValue, sentinelNestedValue, sentinelEmbeddedExcerptData} {
			if strings.Contains(strings.Replace(text, sentinelExcerptMarker, "", 1), sentinel) {
				t.Fatalf("private-triage bundle leaks query/response sentinel %q outside the payload attachment", sentinel)
			}
		}

		if err := Validate(bundle, ValidateOptions{RequirePublic: true}); err == nil {
			t.Fatalf("Validate(private-triage bundle, RequirePublic=true) error = nil, want rejection")
		}
		if err := Validate(bundle, ValidateOptions{}); err != nil {
			t.Fatalf("Validate(private-triage bundle) error = %v, want nil (rest of bundle must still pass the share-safe gate)", err)
		}
	})
}

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
