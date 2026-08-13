// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
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
	egressMalformedSentinel     = "EGRESS-MALFORMED-QUERY-d0e4b8"
	egressNoteQuerySentinel     = "EGRESS-NOTE-QUERY-5b82ea"
	egressNoteHeaderSentinel    = "EGRESS-NOTE-HEADER-c419d7"
	egressNoteAPIKeyHeaderToken = "EGRESS-NOTE-XAPIKEY-38fb6c"
	egressNoteSecondLineToken   = "EGRESS-NOTE-LINE2-7d0aa4"
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
	name             string
	target           string
	params           map[string]any
	errorDetails     map[string]any
	note             string
	sentinels        []string
	wantCaptureError bool
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
			name:         "error details echo a caller selector carrying a credential",
			target:       "/api/v0/services/checkout/story",
			errorDetails: map[string]any{"selector": "checkout?api_key=" + egressErrorDetailSentinel},
			sentinels:    []string{egressErrorDetailSentinel},
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
	if tc.errorDetails != nil {
		input.Envelope.Error = &query.ErrorEnvelope{
			Code:    "ambiguous",
			Message: "selector matched more than one service",
			Details: tc.errorDetails,
		}
	}
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
	if tc.errorDetails != nil {
		bundle.Response.Error = &query.ErrorEnvelope{
			Code:    "ambiguous",
			Message: "selector matched more than one service",
			Details: tc.errorDetails,
		}
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
