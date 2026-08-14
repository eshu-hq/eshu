// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// This file holds the full-egress canary. The older canary in
// redaction_canary_test.go greps the serialized bundle only, which is why an
// error message that echoed the reporter's whole target survived three review
// rounds: nothing in the suite ever read a returned error.
//
// The rule proved here is the package boundary, not one code path. A planted
// sentinel must not appear in ANY output this package produces for a given
// input — the marshaled bundle and the .Error() string of whatever error came
// back — across Capture and Validate, on the success path and the
// parse-failure path.
//
// Every sentinel and endpoint below is synthetic.
//
// KNOWN GAP, measured and not closed here: every sentinel in the TABLE below is
// planted in a VALUE. A credential can also be planted in a KEY —
// url.ParseQuery percent-decodes key names, so
// `--endpoint '/api/v0/x?api_key%3Dsk-live-X'` parses to one parameter whose
// NAME is literally `api_key=sk-live-X`.
//
// Half of that is now covered, by TestValidateDoesNotEchoAUserSuppliedKeyName
// further down: Validate's own rejection messages no longer quote a key that is
// itself query-shaped. Two routes are still open, and neither belongs to this
// change:
//
//   - Capture drops the key and copies that raw name into Redaction.Rules, a
//     []string of values collector's key walk never inspects, so the credential
//     ships inside a bundle stamped public/passed. What Rules should record
//     when the dropped key IS the credential is an open decision.
//   - A sensitive param NAME inside query.params is handed to the share-safe
//     gate on purpose (see validateQueryInputs), and that gate quotes the key
//     it rejected: `sensitive-looking key "query.params.api_key=sk-live-X" must
//     be redacted before emission`. The message belongs to sdk/go/collector.
//
// So a key-planted sentinel in this table would still be red on the Capture
// channel. Add it as part of the change that closes those two, not before.

const (
	egressTargetQuerySentinel   = "EGRESS-TARGET-QUERY-9a1f42"
	egressNestedTargetSentinel  = "EGRESS-NESTED-TARGET-4c7d10"
	egressExplicitParamSentinel = "EGRESS-EXPLICIT-PARAM-2e58c3"
	egressEncodedParamSentinel  = "EGRESS-ENCODED-PARAM-1d9f64"
	egressNestedParamSentinel   = "EGRESS-NESTED-PARAM-b36a97"
	egressErrorDetailSentinel   = "EGRESS-ERROR-DETAIL-71cf05"
	egressErrorMessageSentinel  = "EGRESS-ERROR-MESSAGE-6ba3e2"
	egressErrorCorrelationToken = "EGRESS-ERROR-CORRELATION-af57d1"
	egressMalformedSentinel     = "EGRESS-MALFORMED-QUERY-d0e4b8"
	egressNoteQuerySentinel     = "EGRESS-NOTE-QUERY-5b82ea"
	egressNoteHeaderSentinel    = "EGRESS-NOTE-HEADER-c419d7"
	egressNoteAPIKeyHeaderToken = "EGRESS-NOTE-XAPIKEY-38fb6c"
	egressNoteSecondLineToken   = "EGRESS-NOTE-LINE2-7d0aa4"
	egressNoteMixedQueryToken   = "EGRESS-NOTE-MIXED-QUERY-3fa612"
	egressNoteMixedHeaderToken  = "EGRESS-NOTE-MIXED-HEADER-8c04de"
	egressEncodedSelectorToken  = "EGRESS-ENCODED-SELECTOR-2a71f9"
	egressEncodedNoteToken      = "EGRESS-ENCODED-NOTE-c58d03"
	egressEncodedHeaderToken    = "EGRESS-ENCODED-HEADER-90ba2c"
	egressContinuedNoteToken    = "EGRESS-CONTINUED-NOTE-6e4b17"
)

// assertNoEgress is the whole point of this file: it concatenates every channel
// this package can return a value on — the serialized bundle plus the error
// text — and fails if a sentinel shows up in either. A test that checked only
// one of the two is what let the error echo through.
func assertNoEgress(t *testing.T, label string, bundle Bundle, err error, sentinels ...string) {
	t.Helper()
	var egress strings.Builder
	egress.Write(mustMarshal(t, bundle))
	if err != nil {
		egress.WriteString("\n--- returned error ---\n")
		egress.WriteString(err.Error())
	}
	text := egress.String()
	for _, sentinel := range sentinels {
		if strings.Contains(text, sentinel) {
			t.Errorf("%s: sentinel %q escaped in package output:\n%s", label, sentinel, text)
		}
	}
}

// egressCase is one planted sentinel and the reporter-typed query input that
// carries it. wantCaptureError marks the inputs Capture is expected to refuse
// outright rather than redact parameter by parameter.
type egressCase struct {
	name   string
	target string
	params map[string]any
	// errorSelector is the caller-typed service selector the ambiguous path was
	// given. A case that sets it gets BOTH halves of the real envelope built
	// from that one string — details.selector and the composed Message — so no
	// case can plant a sentinel in the structured half while the sentence beside
	// it quietly holds a constant. See ambiguousErrorEnvelope.
	errorSelector      string
	errorCorrelationID string
	note               string
	sentinels          []string
	wantCaptureError   bool
}

// composedAmbiguousMessage mirrors the sentence
// query/service_workload_resolution.go:39 builds for an ambiguous selector. The
// prose is not what is under test — the SELECTOR being interpolated into it is,
// and that the production code really does interpolate it is pinned on the
// producing side by
// query.TestServiceStoryAmbiguousEnvelopeCarriesSelectorInMessage. If that test
// goes red, this constant is describing something the server no longer sends and
// this file's error cases stop meaning anything.
func composedAmbiguousMessage(selector string) string {
	return fmt.Sprintf(
		"service selector %q matched multiple services; add service_id, repo, or environment",
		selector,
	)
}

// ambiguousErrorEnvelope is the one builder both the Capture half and the
// Validate half of this file use, so a bundle Capture is asked to redact and a
// bundle Validate is asked to reject can never be built from different bytes.
//
// It is also why the error cases can fail at all. Both builders used to hardcode
// Message to a fixed sentence carrying no sentinel, which made every assertion
// about Message structurally incapable of going red — the leak this file exists
// to catch shipped underneath a green run of this very test.
func (tc egressCase) ambiguousErrorEnvelope() *query.ErrorEnvelope {
	if tc.errorSelector == "" && tc.errorCorrelationID == "" {
		return nil
	}
	envelope := &query.ErrorEnvelope{
		Code:          "ambiguous",
		Capability:    "platform_impact.context_overview",
		CorrelationID: tc.errorCorrelationID,
	}
	if tc.errorSelector == "" {
		envelope.Message = "selector matched more than one service"
		return envelope
	}
	envelope.Message = composedAmbiguousMessage(tc.errorSelector)
	envelope.Details = map[string]any{
		"status":   "ambiguous",
		"selector": tc.errorSelector,
	}
	return envelope
}

func egressCases() []egressCase {
	return []egressCase{
		{
			name:      "sensitive-named parameter in the target query string",
			target:    "/api/v0/services/checkout/story?api_key=" + egressTargetQuerySentinel + "&repo=demo%2Fservice",
			sentinels: []string{egressTargetQuerySentinel},
		},
		{
			name:      "nested second ? hides the credential inside a benign target parameter",
			target:    "/api/v0/services/checkout/story?next=/api/v0/x?api_key=" + egressNestedTargetSentinel,
			sentinels: []string{egressNestedTargetSentinel},
		},
		{
			name:      "explicit --params carries a query-shaped value under a benign name",
			target:    "/api/v0/services/checkout/story",
			params:    map[string]any{"next": "/api/v0/x?api_key=" + egressExplicitParamSentinel},
			sentinels: []string{egressExplicitParamSentinel},
		},
		{
			// The same trick as the case above, written the way a browser or an
			// HTTP client writes it. Nothing decodes a --params value on its way
			// in: the target's query string is decoded by url.ParseQuery, so
			// "%3F" arrives as "?" there, while --params hands the bytes over
			// untouched. That difference used to decide the verdict, which is
			// exactly the field-by-field asymmetry this package exists to remove.
			name:      "percent-encoded nested query inside an explicit --params value",
			target:    "/api/v0/services/checkout/story",
			params:    map[string]any{"next": "/api/v0/x%3Fapi_key%3D" + egressEncodedParamSentinel},
			sentinels: []string{egressEncodedParamSentinel},
		},
		{
			name:   "nested value inside a --params object",
			target: "/api/v0/services/checkout/story",
			params: map[string]any{
				"filters": map[string]any{
					"redirect": []any{"/api/v0/x?access_token=" + egressNestedParamSentinel},
				},
			},
			sentinels: []string{egressNestedParamSentinel},
		},
		{
			name:          "error details echo a caller selector carrying a credential",
			target:        "/api/v0/services/checkout/story",
			errorSelector: "checkout?api_key=" + egressErrorDetailSentinel,
			sentinels:     []string{egressErrorDetailSentinel},
		},
		{
			// The same selector, read off the OTHER field of the same envelope.
			// The redactor already fired rule "selector" on this value and
			// dropped it from Details, then shipped it verbatim in the sentence
			// composed from it — a bundle stamped profile=public and
			// validation=passed with the credential still in
			// response.error.message.
			name:          "composed error message interpolates a caller selector carrying a credential",
			target:        "/api/v0/services/checkout/story",
			errorSelector: "checkout?token=" + egressErrorMessageSentinel,
			sentinels:     []string{egressErrorMessageSentinel},
		},
		{
			// correlation_id is caller-controlled: documentation.go:470 returns
			// the request's own X-Correlation-ID header verbatim, and auth.go:430
			// puts it in an error envelope without the character allowlist the
			// audit path applies.
			name:               "error correlation id repeats a caller-supplied header carrying a credential",
			target:             "/api/v0/services/checkout/story",
			errorCorrelationID: "corr-1?access_token=" + egressErrorCorrelationToken,
			sentinels:          []string{egressErrorCorrelationToken},
		},
		{
			name:             "malformed query string alongside the credential",
			target:           "/api/v0/services/checkout/story?api_key=" + egressMalformedSentinel + "&bad=%ZZ",
			sentinels:        []string{egressMalformedSentinel},
			wantCaptureError: true,
		},
		{
			name:      "reporter note pastes a curl whose URL carries a credential",
			target:    "/api/v0/services/checkout/story",
			note:      "ran this and got an empty list:\ncurl 'https://eshu.example/api/v0/x?api_key=" + egressNoteQuerySentinel + "&repo=demo%2Fservice'",
			sentinels: []string{egressNoteQuerySentinel},
		},
		{
			// The same composed message as the row above, with the selector
			// spelled the way an HTTP client writes it. Until the free-text scan
			// read a "%3D" as an "=", every encoded row in this file sat in
			// `params` — the structured domain, which has decoded since it was
			// written — so the axis looked covered and the free-text domain had
			// never been asked about it once.
			name:          "composed error message interpolates a percent-encoded caller selector",
			target:        "/api/v0/services/checkout/story",
			errorSelector: "checkout%3Ftoken%3D" + egressEncodedSelectorToken,
			sentinels:     []string{egressEncodedSelectorToken},
		},
		{
			// The OAuth callback as a browser writes it, pasted into a note.
			// "?redirect_uri=/cb?access_token=…" is one of the three credentials
			// the shared separator constant was introduced for; this is the same
			// URL, percent-encoded.
			name:      "reporter note pastes a curl whose URL carries a percent-encoded nested credential",
			target:    "/api/v0/services/checkout/story",
			note:      "ran: curl 'https://eshu.example/api/v0/x?redirect_uri=%2Fcb%3Faccess_token%3D" + egressEncodedNoteToken + "'",
			sentinels: []string{egressEncodedNoteToken},
		},
		{
			// The header form's own encoded spelling. The pair form got a row
			// first and this one did not, which a break-the-line probe caught:
			// reading the ":" through an escape could be deleted outright and
			// the whole suite stayed green.
			name:      "reporter note pastes a header whose colon is percent-encoded",
			target:    "/api/v0/services/checkout/story",
			note:      "ran: curl -s -H 'Authorization%3A Bearer " + egressEncodedHeaderToken + "' https://eshu.example/api/v0/x",
			sentinels: []string{egressEncodedHeaderToken},
		},
		{
			// A pasted curl wrapped mid-header. Every rule in the free-text scan
			// stops at a newline, so the header rule removed the empty remainder
			// of the first line and left the credential alone on the second.
			name:      "reporter note wraps a header value onto a continuation line",
			target:    "/api/v0/services/checkout/story",
			note:      "curl -s https://eshu.example/api/v0/x \\\n  -H 'Authorization: \\\n  Bearer " + egressContinuedNoteToken + "'",
			sentinels: []string{egressContinuedNoteToken},
		},
		{
			name:      "reporter note pastes a curl carrying an Authorization header",
			target:    "/api/v0/services/checkout/story",
			note:      "curl -s -H 'Authorization: Bearer " + egressNoteHeaderSentinel + "' https://eshu.example/api/v0/x",
			sentinels: []string{egressNoteHeaderSentinel},
		},
		{
			name:      "reporter note pastes an X-Api-Key header on its own line",
			target:    "/api/v0/services/checkout/story",
			note:      "the owner list came back empty.\n  -H \"X-Api-Key: " + egressNoteAPIKeyHeaderToken + "\"\nsame result on a retry.",
			sentinels: []string{egressNoteAPIKeyHeaderToken},
		},
		{
			name:      "reporter note carries a credential on a later line",
			target:    "/api/v0/services/checkout/story",
			note:      "expected the platform team as owner.\nrepro: GET /api/v0/x?access_token=" + egressNoteSecondLineToken,
			sentinels: []string{egressNoteSecondLineToken},
		},
		{
			// Both shapes on one line, which is what a real pasted curl looks
			// like. The header rule truncates the line, so the query pair in
			// front of it is only cleaned if the prefix is scanned too. It was
			// not, and the leftover pair made the scan non-idempotent: Validate
			// re-ran it, saw the text change, and Capture refused to emit the
			// bundle at all. The reporter got nothing.
			name:      "reporter note pastes a curl carrying a credential in both the query and a header",
			target:    "/api/v0/services/checkout/story",
			note:      "curl 'https://eshu.example/api/v0/x?token=" + egressNoteMixedQueryToken + "' -H 'X-Api-Key: " + egressNoteMixedHeaderToken + "'",
			sentinels: []string{egressNoteMixedQueryToken, egressNoteMixedHeaderToken},
		},
	}
}

func (tc egressCase) captureInput() CaptureInput {
	input := CaptureInput{
		Surface: "api",
		Target:  tc.target,
		Method:  "GET",
		Params:  tc.params,
		Envelope: query.ResponseEnvelope{
			Data:  map[string]any{"owner": "platform-team"},
			Truth: &query.TruthEnvelope{Level: query.TruthLevelExact, Basis: query.TruthBasisAuthoritativeGraph},
		},
		ReporterNote: "expected the owning team, got an empty list",
	}
	if tc.note != "" {
		input.ReporterNote = tc.note
	}
	input.Envelope.Error = tc.ambiguousErrorEnvelope()
	return input
}

// TestCaptureFullEgressCanary plants a sentinel in each reporter-typed query
// input and proves Capture returns it in neither the bundle nor the error.
func TestCaptureFullEgressCanary(t *testing.T) {
	t.Parallel()

	for _, tc := range egressCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bundle, err := Capture(tc.captureInput())
			switch {
			case tc.wantCaptureError && err == nil:
				t.Fatalf("Capture() error = nil, want refusal for %s", tc.name)
			case !tc.wantCaptureError && err != nil:
				t.Fatalf("Capture() error = %v, want a redacted bundle for %s", err, tc.name)
			}
			assertNoEgress(t, "Capture("+tc.name+")", bundle, err, tc.sentinels...)
		})
	}
}

// handEditedBundle builds a bundle carrying the case's raw query inputs without
// going through Capture, which is how a bundle actually arrives at a
// maintainer: attached to an issue, possibly hand-edited, possibly written by a
// third-party tool. Validate has to find the leak without trusting the
// producer, and its rejection message must not repeat the credential either.
func (tc egressCase) handEditedBundle(t *testing.T) Bundle {
	t.Helper()
	bundle := minimalPublicBundle(t)
	bundle.Query.Target = tc.target
	if tc.params != nil {
		bundle.Query.Params = tc.params
	}
	if tc.note != "" {
		bundle.ReporterNote = tc.note
	}
	if envelope := tc.ambiguousErrorEnvelope(); envelope != nil {
		bundle.Response.Error = envelope
	}
	return bundle
}

// TestValidateFullEgressCanary is the half that catches a bundle Capture never
// touched. It also pins the invariant the package rests on: Validate rejects
// exactly the inputs Capture redacts, so a Capture-produced bundle can never
// disagree with its own validator.
func TestValidateFullEgressCanary(t *testing.T) {
	t.Parallel()

	for _, tc := range egressCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bundle := tc.handEditedBundle(t)
			err := Validate(bundle, ValidateOptions{})
			if err == nil {
				t.Fatalf("Validate(hand-edited bundle carrying %s) error = nil, want rejection", tc.name)
			}
			// Only the error is checked for egress here. The bundle is the
			// tester's own unredacted input, so of course it holds the
			// sentinel; what must not leak is Validate's report about it.
			for _, sentinel := range tc.sentinels {
				if strings.Contains(err.Error(), sentinel) {
					t.Errorf("Validate(%s) error echoes the sentinel %q: %s", tc.name, sentinel, err.Error())
				}
			}
		})
	}
}

// egressValidateKeySentinel is planted in a map KEY rather than a value, which
// is the one place the table above cannot reach: every case there uses a fixed
// parameter name, so no assertion in this file ever looked at what happens when
// the reporter's own text IS the key.
const egressValidateKeySentinel = "EGRESS-VALIDATE-KEY-8d24fa"

// TestValidateDoesNotEchoAUserSuppliedKeyName covers the same class of leak the
// rest of this file covers, on the other half of a key-value pair. Validate
// reports WHERE it found an embedded credential, and it used to build that
// location by concatenating the bundle's own map keys — so a bundle whose key
// was itself credential-shaped got that key quoted back into the rejection
// message, and from there into a terminal or a CI log.
//
// That shape is not hypothetical: url.ParseQuery percent-decodes key names, so
// `--endpoint '/x?api_key%3Dsk-live-...'` parses to a single parameter whose
// NAME is literally "api_key=sk-live-...". Both routes below are checked,
// because a message that stops echoing on one of them and not the other is the
// field-by-field asymmetry this package exists to remove.
//
// The residual limit is the package's usual one: a key is judged by NAME. A
// credential sitting under a benign-looking key name is still repeated, exactly
// as a benign-named parameter's value is still kept.
func TestValidateDoesNotEchoAUserSuppliedKeyName(t *testing.T) {
	t.Parallel()

	// The VALUE has to carry an embedded pair too, or Validate rejects the
	// bundle later (at the share-safe gate) on a different message and the path
	// builder under test never runs.
	const carrier = "/api/v0/x?access_token=sk-live-placeholder"

	placements := []struct {
		field  string
		mutate func(*Bundle)
	}{
		{
			field: "query.params",
			mutate: func(b *Bundle) {
				b.Query.Params = map[string]any{"api_key=" + egressValidateKeySentinel: carrier}
			},
		},
		{
			field: "query.target",
			mutate: func(b *Bundle) {
				b.Query.Target = "/api/v0/services/checkout/story?api_key%3D" + egressValidateKeySentinel + "=v"
			},
		},
	}

	for _, placement := range placements {
		t.Run(placement.field, func(t *testing.T) {
			t.Parallel()

			bundle := minimalPublicBundle(t)
			placement.mutate(&bundle)

			err := Validate(bundle, ValidateOptions{})
			if err == nil {
				t.Fatalf("Validate(bundle with a credential-shaped %s key) error = nil, want rejection", placement.field)
			}
			if strings.Contains(err.Error(), egressValidateKeySentinel) {
				t.Errorf("Validate(%s) error echoes the key the reporter typed: %s", placement.field, err.Error())
			}
		})
	}
}

// TestCaptureBundleSurvivesValidateAfterRedaction closes the loop the two tests
// above leave open: every case Capture accepts must produce a bundle Validate
// accepts. Without this, widening Validate could start rejecting bundles
// Capture still emits, and each half would look green on its own.
func TestCaptureBundleSurvivesValidateAfterRedaction(t *testing.T) {
	t.Parallel()

	for _, tc := range egressCases() {
		if tc.wantCaptureError {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bundle, err := Capture(tc.captureInput())
			if err != nil {
				t.Fatalf("Capture() error = %v, want nil", err)
			}
			if err := Validate(bundle, ValidateOptions{RequirePublic: true}); err != nil {
				t.Fatalf("Validate(Capture output) error = %v, want nil", err)
			}
		})
	}
}
