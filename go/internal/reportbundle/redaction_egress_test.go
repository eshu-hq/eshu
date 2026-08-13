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
// KNOWN GAP, measured and not closed here: every sentinel in this file is
// planted in a VALUE, so nothing here can catch a credential planted in a KEY.
// url.ParseQuery percent-decodes key names, so
// `--endpoint '/api/v0/x?api_key%3Dsk-live-X'` parses to one parameter whose
// NAME is literally `api_key=sk-live-X`. Capture drops it — and copies that raw
// name into Redaction.Rules, a []string of values collector's key walk never
// inspects, so the credential ships inside a bundle stamped public/passed.
// Validate leaks it a second way: the share-safe gate's own message quotes the
// offending key, which is `sensitive-looking key
// "query.params.api_key=sk-live-X" must be redacted before emission` — an error
// echoing a user-supplied value, against this package's stated error rule.
//
// A key-planted sentinel belongs in this table, and it is deliberately absent:
// added today it would be red on both channels. Closing it needs two decisions
// this change does not own — what Redaction.Rules should record when the
// dropped key is itself the credential, and whether Validate should reject a
// sensitive param NAME itself instead of handing off to the share-safe gate,
// which validateQueryInputs documents as a deliberate no. Plant the sentinel in
// a key as part of that change, not before it.

const (
	egressTargetQuerySentinel   = "EGRESS-TARGET-QUERY-9a1f42"
	egressNestedTargetSentinel  = "EGRESS-NESTED-TARGET-4c7d10"
	egressExplicitParamSentinel = "EGRESS-EXPLICIT-PARAM-2e58c3"
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

// symmetrySentinel is the credential in the placement-symmetry control below.
const symmetrySentinel = "SYMMETRY-CONTROL-6e13af"

// symmetryBytes is one byte sequence, placed in two reporter-typed fields. It
// is the exact shape the audit used: a benign-looking "next" parameter whose
// value hides a second query string carrying the credential.
const symmetryBytes = "next=/api/v0/x?api_key=" + symmetrySentinel

// TestReporterInputPlacementSymmetry pins the defect this change closed. The
// same bytes used to be rejected in query.target and accepted in
// reporter_note — one verdict per field, for text the same person typed. The
// asymmetry was the bug, so symmetry is what guards it.
//
// It compares the verdict, not the mechanics: whether the sentinel escapes,
// whether the removal is recorded, and whether Validate rejects a bundle
// somebody hand-wrote with those bytes in place. How each field is scanned
// differs (a query string is re-parsed into pairs, free text is scanned line by
// line) and that is fine; what must not differ is the answer.
func TestReporterInputPlacementSymmetry(t *testing.T) {
	t.Parallel()

	placements := []struct {
		field    string
		capture  CaptureInput
		handEdit func(*Bundle)
	}{
		{
			field: "query.target",
			capture: CaptureInput{
				Surface:      "api",
				Target:       "/api/v0/services/checkout/story?" + symmetryBytes,
				Method:       "GET",
				Envelope:     query.ResponseEnvelope{Data: map[string]any{"owner": "platform-team"}},
				ReporterNote: "expected the owning team, got an empty list",
			},
			handEdit: func(b *Bundle) {
				b.Query.Target = "/api/v0/services/checkout/story?" + symmetryBytes
			},
		},
		{
			field: "reporter_note",
			capture: CaptureInput{
				Surface:      "api",
				Target:       "/api/v0/services/checkout/story",
				Method:       "GET",
				Envelope:     query.ResponseEnvelope{Data: map[string]any{"owner": "platform-team"}},
				ReporterNote: symmetryBytes,
			},
			handEdit: func(b *Bundle) { b.ReporterNote = symmetryBytes },
		},
	}

	for _, placement := range placements {
		t.Run(placement.field, func(t *testing.T) {
			t.Parallel()

			bundle, err := Capture(placement.capture)
			if err != nil {
				t.Fatalf("Capture(credential in %s) error = %v, want a redacted bundle", placement.field, err)
			}
			assertNoEgress(t, "Capture(credential in "+placement.field+")", bundle, err, symmetrySentinel)
			if len(bundle.Redaction.Rules) == 0 {
				t.Errorf("Capture(credential in %s): Redaction.Rules is empty, want the removal recorded", placement.field)
			}
			if err := Validate(bundle, ValidateOptions{RequirePublic: true}); err != nil {
				t.Errorf("Validate(Capture output for %s) error = %v, want nil", placement.field, err)
			}

			handWritten := minimalPublicBundle(t)
			placement.handEdit(&handWritten)
			err = Validate(handWritten, ValidateOptions{RequirePublic: true})
			if err == nil {
				t.Fatalf("Validate(hand-written bundle with the credential in %s) error = nil, want rejection", placement.field)
			}
			if strings.Contains(err.Error(), symmetrySentinel) {
				t.Errorf("Validate(%s) error echoes the sentinel: %s", placement.field, err.Error())
			}
		})
	}
}

// TestCaptureKeepsReporterNoteAroundTheCredential proves the note scan removes
// the credential without throwing away the report. The note is the reporter's
// own account of what they expected, and a pasted repro with the secret cut out
// is still the repro — dropping the whole field would cost a maintainer the most
// useful prose in the bundle for both a real hit and a false one.
func TestCaptureKeepsReporterNoteAroundTheCredential(t *testing.T) {
	t.Parallel()

	bundle, err := Capture(CaptureInput{
		Surface:  "api",
		Target:   "/api/v0/services/checkout/story",
		Method:   "GET",
		Envelope: query.ResponseEnvelope{Data: map[string]any{"owner": "platform-team"}},
		ReporterNote: "expected the platform team, got an empty list.\n" +
			"curl 'https://eshu.example/api/v0/x?repo=demo&api_key=" + egressNoteQuerySentinel + "'\n" +
			"same on a retry.",
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	for _, keep := range []string{
		"expected the platform team, got an empty list.",
		"https://eshu.example/api/v0/x?repo=demo",
		"same on a retry.",
	} {
		if !strings.Contains(bundle.ReporterNote, keep) {
			t.Errorf("ReporterNote lost %q; got:\n%s", keep, bundle.ReporterNote)
		}
	}
	if strings.Contains(bundle.ReporterNote, egressNoteQuerySentinel) {
		t.Errorf("ReporterNote kept the credential: %s", bundle.ReporterNote)
	}
	if !containsRule(bundle.Redaction.Rules, "reporter_note") {
		t.Errorf("Redaction.Rules = %v, want it to record reporter_note", bundle.Redaction.Rules)
	}
}

// TestCaptureLeavesACleanReporterNoteAlone is the false-positive guard. A note
// with no key=value or header pair in it must arrive byte for byte, and must not
// be listed in Redaction.Rules — a rules entry tells a maintainer something was
// removed, so recording one when nothing was is its own bug.
func TestCaptureLeavesACleanReporterNoteAlone(t *testing.T) {
	t.Parallel()

	note := "expected the platform team as owner of demo/service, got [].\n" +
		"the CODEOWNERS file has one entry and it points at @platform.\n" +
		"tried it against pkg:deb/debian/openssl@3.0.11-1?arch=amd64 too."
	bundle, err := Capture(CaptureInput{
		Surface:      "api",
		Target:       "/api/v0/services/checkout/story",
		Method:       "GET",
		Envelope:     query.ResponseEnvelope{Data: map[string]any{"owner": "platform-team"}},
		ReporterNote: note,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	if bundle.ReporterNote != note {
		t.Errorf("ReporterNote was altered.\n got: %s\nwant: %s", bundle.ReporterNote, note)
	}
	if containsRule(bundle.Redaction.Rules, "reporter_note") {
		t.Errorf("Redaction.Rules = %v, want no reporter_note entry for an untouched note", bundle.Redaction.Rules)
	}
}

func containsRule(rules []string, want string) bool {
	for _, rule := range rules {
		if rule == want {
			return true
		}
	}
	return false
}

// A parameter whose value merely looks query-shaped is not a credential.
// Package URLs are the real case: `package_id` on the supply-chain routes
// accepts a PURL whose qualifier segment is literally "?arch=amd64&distro=...",
// and dropping it would cost a maintainer the parameter the report is about.
// Only an embedded pair whose KEY is sensitive-named triggers removal.
func TestCaptureKeepsQueryShapedBenignParams(t *testing.T) {
	t.Parallel()

	purl := "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12"
	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  "/api/v0/supply-chain/impact/explain",
		Method:  "GET",
		Params: map[string]any{
			"package_id": purl,
			"filters":    map[string]any{"nested": "a=b&c=d"},
		},
		Envelope: query.ResponseEnvelope{Data: map[string]any{"ok": true}},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	if got := bundle.Query.Params["package_id"]; got != purl {
		t.Errorf("Query.Params[\"package_id\"] = %#v, want the qualified PURL kept verbatim", got)
	}
	nested, ok := bundle.Query.Params["filters"].(map[string]any)
	if !ok {
		t.Fatalf("Query.Params[\"filters\"] = %#v, want a nested object kept", bundle.Query.Params["filters"])
	}
	if got := nested["nested"]; got != "a=b&c=d" {
		t.Errorf("Query.Params[\"filters\"][\"nested\"] = %#v, want the benign query-shaped value kept", got)
	}
}
