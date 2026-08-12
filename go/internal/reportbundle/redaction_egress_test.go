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

const (
	egressTargetQuerySentinel   = "EGRESS-TARGET-QUERY-9a1f42"
	egressNestedTargetSentinel  = "EGRESS-NESTED-TARGET-4c7d10"
	egressExplicitParamSentinel = "EGRESS-EXPLICIT-PARAM-2e58c3"
	egressNestedParamSentinel   = "EGRESS-NESTED-PARAM-b36a97"
	egressErrorDetailSentinel   = "EGRESS-ERROR-DETAIL-71cf05"
	egressMalformedSentinel     = "EGRESS-MALFORMED-QUERY-d0e4b8"
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
