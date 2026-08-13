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
