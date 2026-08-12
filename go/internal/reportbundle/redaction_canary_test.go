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

// This file holds the redaction canaries: the acceptance-criterion tests that
// prove sensitive values never reach a share-safe bundle. They live apart from
// capture_test.go so the redaction proof reads as one suite, and so neither
// file approaches the repository's 500-line cap.

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
	sentinelTargetQueryValue    = "CANARY-TARGET-QUERY-SENTINEL-a7e3b1"
)

// targetWithSensitiveQueryString is the one place the target-query-string
// canary input is written, so the Capture test and the Validate test below
// exercise the same string rather than two copies that can drift apart.
func targetWithSensitiveQueryString() string {
	return "/api/v0/services/checkout/story?api_key=" + sentinelTargetQueryValue + "&repo=demo%2Fservice"
}

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

// TestCapture_RedactsSensitiveTargetQueryString closes a gap the original
// redaction canary could not see. Every redaction rule in this package walks
// map KEYS, and the share-safe gate (collector.ValidateShareSafeKeys) also
// judges key names only — it never looks at a string VALUE. Query.Target is a
// plain string field that Capture stored verbatim, and `eshu report capture`
// passes whatever the reporter typed after --endpoint straight into it. So
// `--endpoint "/path?api_key=sk-live-..."` put a live credential inside a
// bundle labeled "public", and both Capture's own fail-closed Validate call and
// a maintainer's `eshu report validate --require-public` passed it.
//
// Capture must split the query string off the target and route its parameters
// through the same key-name redaction walk Params already gets. The benign
// parameters have to survive that split: a maintainer cannot reproduce the
// wrong answer from a bundle whose reproduction inputs were thrown away.
func TestCapture_RedactsSensitiveTargetQueryString(t *testing.T) {
	t.Parallel()

	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  targetWithSensitiveQueryString(),
		Method:  "GET",
		Envelope: query.ResponseEnvelope{
			Data:  map[string]any{"owner": "platform-team"},
			Truth: &query.TruthEnvelope{Level: query.TruthLevelExact},
		},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}

	if bundle.Query.Target != "/api/v0/services/checkout/story" {
		t.Errorf("Query.Target = %q, want the bare path with the query string split off", bundle.Query.Target)
	}

	// The teeth: the sentinel must not survive anywhere in the serialized
	// bytes, the same substring standard TestCapture_RedactionCanary applies.
	if text := string(mustMarshal(t, bundle)); strings.Contains(text, sentinelTargetQueryValue) {
		t.Errorf("serialized public bundle leaks the target query-string sentinel %q:\n%s", sentinelTargetQueryValue, text)
	}

	// Reproduction inputs survive: the benign parameter is merged into Params,
	// percent-decoded, so the split does not cost the maintainer the query.
	if got := bundle.Query.Params["repo"]; got != "demo/service" {
		t.Errorf("Query.Params[\"repo\"] = %#v, want the decoded target query-string parameter merged in", got)
	}
	if _, present := bundle.Query.Params["api_key"]; present {
		t.Error("Query.Params carries the sensitive target parameter; it must be dropped like any other sensitive key")
	}

	// The removal is documented rather than silent.
	if !containsString(bundle.Redaction.Rules, "api_key") {
		t.Errorf("Redaction.Rules = %v, want it to record the stripped \"api_key\" target parameter", bundle.Redaction.Rules)
	}
}

// An explicitly supplied --params value wins over a same-named parameter
// parsed out of the target's query string. The explicit flag is the more
// deliberate input, and silently overwriting it would misreport what was asked.
func TestCapture_ExplicitParamsWinOverTargetQueryString(t *testing.T) {
	t.Parallel()

	bundle, err := Capture(CaptureInput{
		Surface:  "api",
		Target:   "/api/v0/services/checkout/story?repo=from-target",
		Params:   map[string]any{"repo": "from-flag"},
		Envelope: query.ResponseEnvelope{Data: map[string]any{"ok": true}},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	if got := bundle.Query.Params["repo"]; got != "from-flag" {
		t.Errorf("Query.Params[\"repo\"] = %#v, want the explicit --params value to win", got)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
