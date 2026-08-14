// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// This file is the other half of the egress canary in
// redaction_egress_test.go. That one proves nothing a reporter typed escapes;
// these prove the two things a redactor gets wrong in the other direction —
// deciding a verdict by which FIELD held the bytes, and taking away more than
// the credential.
//
// A redaction that drops the whole note, or the parameter the report is about,
// is not a safer redaction. It is a bundle nobody can reproduce a wrong answer
// from, so the false-positive cases here are load-bearing, not decoration.

// symmetrySentinel is the credential in the placement-symmetry control below.
const symmetrySentinel = "SYMMETRY-CONTROL-6e13af"

// symmetrySpellings are the byte sequences placed in each reporter-typed field.
// Every spelling goes into every placement, so the test is a grid rather than a
// list.
//
// There used to be ONE constant here, and that is how the encoding axis got
// through: flipping it to its percent-encoded spelling turned query.target
// green — the structured domain decodes — while reporter_note and
// response.error.message both leaked, which is the exact "one verdict per field,
// for text the same person typed" property this test claims to guard. A single
// byte pattern lets one fix close the axis for every placement at once, so a
// placement the fix missed looks covered.
var symmetrySpellings = []struct {
	name  string
	bytes string
}{
	{
		// The shape the original audit used: a benign-looking "next" parameter
		// whose value hides a second query string carrying the credential.
		name:  "literal",
		bytes: "next=/api/v0/x?api_key=" + symmetrySentinel,
	},
	{
		// The same URL as a browser or an HTTP client writes it. No literal "?"
		// and no literal "=" inside the value for a split to find.
		name:  "percent-encoded",
		bytes: "next=%2Fapi%2Fv0%2Fx%3Fapi%5Fkey%3D" + symmetrySentinel,
	},
	{
		// A different carrier again: the separator that ends the pair, encoded,
		// with text after the credential so the boundary has to be found.
		name:  "percent-encoded with a following parameter",
		bytes: "a=1%26token%3D" + symmetrySentinel + "%26repo%3Ddemo",
	},
}

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
		capture  func(bytes string) CaptureInput
		handEdit func(b *Bundle, bytes string)
	}{
		{
			field: "query.target",
			capture: func(bytes string) CaptureInput {
				return CaptureInput{
					Surface:      "api",
					Target:       "/api/v0/services/checkout/story?" + bytes,
					Method:       "GET",
					Envelope:     query.ResponseEnvelope{Data: map[string]any{"owner": "platform-team"}},
					ReporterNote: "expected the owning team, got an empty list",
				}
			},
			handEdit: func(b *Bundle, bytes string) {
				b.Query.Target = "/api/v0/services/checkout/story?" + bytes
			},
		},
		{
			field: "reporter_note",
			capture: func(bytes string) CaptureInput {
				return CaptureInput{
					Surface:      "api",
					Target:       "/api/v0/services/checkout/story",
					Method:       "GET",
					Envelope:     query.ResponseEnvelope{Data: map[string]any{"owner": "platform-team"}},
					ReporterNote: bytes,
				}
			},
			handEdit: func(b *Bundle, bytes string) { b.ReporterNote = bytes },
		},
		{
			// The third placement is the one the first two did not imply. This
			// field is written by the SERVER, so it read as out of scope — but
			// the server writes it by interpolating the caller's own selector,
			// which is the same bytes arriving by a longer route. Same text,
			// same person typed it, so it must get the same verdict.
			field: "response.error.message",
			capture: func(bytes string) CaptureInput {
				return CaptureInput{
					Surface: "api",
					Target:  "/api/v0/services/checkout/story",
					Method:  "GET",
					Envelope: query.ResponseEnvelope{
						Data: map[string]any{"owner": "platform-team"},
						Error: &query.ErrorEnvelope{
							Code:    "ambiguous",
							Message: composedAmbiguousMessage(bytes),
						},
					},
					ReporterNote: "expected the owning team, got an empty list",
				}
			},
			handEdit: func(b *Bundle, bytes string) {
				b.Response.Error = &query.ErrorEnvelope{
					Code:    "ambiguous",
					Message: composedAmbiguousMessage(bytes),
				}
			},
		},
	}

	for _, placement := range placements {
		for _, spelling := range symmetrySpellings {
			t.Run(placement.field+"/"+spelling.name, func(t *testing.T) {
				t.Parallel()

				label := "Capture(credential in " + placement.field + " as " + spelling.name + ")"
				bundle, err := Capture(placement.capture(spelling.bytes))
				if err != nil {
					t.Fatalf("%s error = %v, want a redacted bundle", label, err)
				}
				assertNoEgress(t, label, bundle, err, symmetrySentinel)
				if len(bundle.Redaction.Rules) == 0 {
					t.Errorf("%s: Redaction.Rules is empty, want the removal recorded", label)
				}
				if err := Validate(bundle, ValidateOptions{RequirePublic: true}); err != nil {
					t.Errorf("Validate(Capture output for %s) error = %v, want nil", label, err)
				}

				handWritten := minimalPublicBundle(t)
				placement.handEdit(&handWritten, spelling.bytes)
				err = Validate(handWritten, ValidateOptions{RequirePublic: true})
				if err == nil {
					t.Fatalf("Validate(hand-written bundle with the credential in %s as %s) error = nil, want rejection",
						placement.field, spelling.name)
				}
				if strings.Contains(err.Error(), symmetrySentinel) {
					t.Errorf("Validate(%s) error echoes the sentinel: %s", label, err.Error())
				}
			})
		}
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

// TestCaptureKeepsTheErrorMessageAroundTheCredential is the note test's
// counterpart on the error envelope, and it guards the direction the leak fix
// could overshoot in. An error message is one short sentence, so dropping the
// whole field on a hit would cost a maintainer the entire explanation of what
// went wrong — and the message is often the only human-readable statement of it
// in the bundle. Only the pair goes; the sentence around it stays, and the
// removal is recorded.
func TestCaptureKeepsTheErrorMessageAroundTheCredential(t *testing.T) {
	t.Parallel()

	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
		Method:  "GET",
		Envelope: query.ResponseEnvelope{
			Data: map[string]any{"owner": "platform-team"},
			Error: &query.ErrorEnvelope{
				Code:    "ambiguous",
				Message: composedAmbiguousMessage("checkout?token=" + egressErrorMessageSentinel),
			},
		},
		ReporterNote: "expected one service, got an ambiguity",
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	if bundle.Response.Error == nil {
		t.Fatal("Response.Error = nil, wanted the envelope kept")
	}
	message := bundle.Response.Error.Message
	if strings.Contains(message, egressErrorMessageSentinel) {
		t.Errorf("Response.Error.Message kept the credential: %s", message)
	}
	for _, keep := range []string{
		"service selector",
		"checkout",
		"matched multiple services; add service_id, repo, or environment",
	} {
		if !strings.Contains(message, keep) {
			t.Errorf("Response.Error.Message lost %q; got: %s", keep, message)
		}
	}
	if !containsRule(bundle.Redaction.Rules, "response_error_message") {
		t.Errorf("Redaction.Rules = %v, want it to record response_error_message", bundle.Redaction.Rules)
	}
}

// TestCaptureLeavesACleanErrorEnvelopeAlone is the false-positive half. The
// realistic ambiguous message — the same composition, with a selector nobody put
// a credential in — must arrive byte for byte and record no rule.
func TestCaptureLeavesACleanErrorEnvelopeAlone(t *testing.T) {
	t.Parallel()

	message := composedAmbiguousMessage("checkout")
	const correlationID = "9f2c41d0a7b34e58bd10c6e2f4a97b31"
	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
		Method:  "GET",
		Envelope: query.ResponseEnvelope{
			Data: map[string]any{"owner": "platform-team"},
			Error: &query.ErrorEnvelope{
				Code:          "ambiguous",
				Message:       message,
				CorrelationID: correlationID,
			},
		},
		ReporterNote: "expected one service, got an ambiguity",
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	if got := bundle.Response.Error.Message; got != message {
		t.Errorf("Response.Error.Message was altered.\n got: %s\nwant: %s", got, message)
	}
	if got := bundle.Response.Error.CorrelationID; got != correlationID {
		t.Errorf("Response.Error.CorrelationID was altered.\n got: %s\nwant: %s", got, correlationID)
	}
	for _, rule := range []string{"response_error_message", "response_error_correlation_id"} {
		if containsRule(bundle.Redaction.Rules, rule) {
			t.Errorf("Redaction.Rules = %v, want no %s entry for an untouched envelope", bundle.Redaction.Rules, rule)
		}
	}
}

// TestCaptureKeepsTheNoteAroundAPercentEncodedCredential is the over-removal
// guard on the encoding axis. Reading a "%3D" as an "=" widened where a pair can
// start, so the cost of getting it wrong is a note cut in the wrong place: the
// URL around the credential has to survive in the spelling the reporter typed,
// escapes and all, or a maintainer cannot reproduce the request.
func TestCaptureKeepsTheNoteAroundAPercentEncodedCredential(t *testing.T) {
	t.Parallel()

	const sentinel = "SYMMETRY-ENCODED-NOTE-41b7c0"
	bundle, err := Capture(CaptureInput{
		Surface:  "api",
		Target:   "/api/v0/services/checkout/story",
		Method:   "GET",
		Envelope: query.ResponseEnvelope{Data: map[string]any{"owner": "platform-team"}},
		ReporterNote: "the owner list came back empty.\n" +
			"curl 'https://eshu.example/api/v0/x?repo=demo%26redirect_uri=%2Fcb%3Faccess_token%3D" + sentinel + "'\n" +
			"same on a retry.",
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	if strings.Contains(bundle.ReporterNote, sentinel) {
		t.Errorf("ReporterNote kept the credential: %s", bundle.ReporterNote)
	}
	for _, keep := range []string{
		"the owner list came back empty.",
		"https://eshu.example/api/v0/x?repo=demo%26redirect_uri=%2Fcb%3F",
		"same on a retry.",
	} {
		if !strings.Contains(bundle.ReporterNote, keep) {
			t.Errorf("ReporterNote lost %q; got:\n%s", keep, bundle.ReporterNote)
		}
	}
}

// TestCaptureStopsAContinuationAtAValueThatEndedMidLine is the false-positive
// guard on the line-continuation rule, pointed at the mechanism rather than
// past it.
//
// A backslash carries a redaction onto the next line only when the removal ran
// to the very end of the line it was on. A wrapped `curl` is exactly where both
// halves matter: every line of it ends in a backslash, and only the one holding
// a bare header value has no boundary of its own.
//
// The note this replaced was "the path was C:\" over two lines of prose. That
// note produced no redaction on its first line, so the continuation decision was
// never taken, and the test stayed green with the redaction's own end-of-line
// check deleted and with the flag hard-wired true. Here the first `--token=`
// line IS redacted and its value ends at the space before the backslash, so
// dropping either half turns the two lines after it into bare markers.
func TestCaptureStopsAContinuationAtAValueThatEndedMidLine(t *testing.T) {
	t.Parallel()

	const sentinel = "SYMMETRY-MIDLINE-CONTINUATION-8ad2f4"
	note := "cmd \\\n" +
		"  --token=" + sentinel + " \\\n" +
		"  --repo=demo \\\n" +
		"  --page=2"
	want := "cmd \\\n" +
		"  [redacted] \\\n" +
		"  --repo=demo \\\n" +
		"  --page=2"

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
	if strings.Contains(bundle.ReporterNote, sentinel) {
		t.Errorf("ReporterNote kept the credential: %s", bundle.ReporterNote)
	}
	if bundle.ReporterNote != want {
		t.Errorf("ReporterNote\n got: %q\nwant: %q", bundle.ReporterNote, want)
	}
	if !containsRule(bundle.Redaction.Rules, "reporter_note") {
		t.Errorf("Redaction.Rules = %v, want a reporter_note entry for a shortened note", bundle.Redaction.Rules)
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
