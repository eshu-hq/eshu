// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file answers one question for every string the freshness commands
// compose or copy into output: can an operator-supplied selector, endpoint, or
// identifier reach it?
//
// The answer splits in two, and both halves are asserted here.
//
//  1. The human summaries render an enumerated set of envelope keys and
//     nothing else. A value the server returns under any other key -- an
//     error's details map, a capability name, an unrecognized data field --
//     never reaches the terminal summary. TestHumanSummariesRenderOnlyKnownKeys
//     plants a sentinel under those keys and asserts its absence.
//
//  2. The error line and --json both reproduce what they were given
//     verbatim. There is no screen on either, by design: --json is the
//     canonical envelope, and a transport error's text is the operator's only
//     clue about which request failed. TestRenderedErrorCarriesTheRequestURL
//     pins that rather than hiding it.
//
// Every absence assertion is paired with a presence assertion on the same
// input, so a run that reports "clean" because the sentinel was never carried
// in the first place fails instead.

const canary = "CANARY-a1b2c3d4e5f6"

// canaryCarriers varies the character immediately before the sentinel. A
// screen keyed on a word or segment boundary passes some of these and fails
// others, so a single spelling would not be evidence.
func canaryCarriers() map[string]string {
	return map[string]string{
		"segment-start": canary,
		"after-letter":  "x" + canary,
		"after-space":   "prefix " + canary,
		"after-at":      "user@" + canary,
		"after-quote":   `"` + canary,
		"after-colon":   "scope:" + canary,
		"after-dot":     "acme." + canary,
		"after-dash":    "acme-" + canary,
		"after-slash":   "acme/" + canary,
	}
}

// TestHumanSummariesRenderOnlyKnownKeys plants the sentinel in envelope members
// the human summaries do not render, and asserts it never reaches them. The
// --json rendering of the same envelope is checked for the sentinel first: if
// that positive control fails, the carrier never held the sentinel and the
// absence result below would mean nothing.
func TestHumanSummariesRenderOnlyKnownKeys(t *testing.T) {
	renderers := map[string]summaryRenderer{
		"generations":           RenderGenerationsSummary,
		"changed-since":         RenderChangedSinceSummary,
		"service-changed-since": RenderServiceChangedSinceSummary,
	}

	for carrier, value := range canaryCarriers() {
		for name, render := range renderers {
			t.Run(carrier+"/"+name, func(t *testing.T) {
				env := envelopeWithHiddenSentinel(value)

				jsonOut := &bytes.Buffer{}
				if err := WriteJSON(jsonOut, env); err != nil {
					t.Fatalf("WriteJSON() error = %v", err)
				}
				if !strings.Contains(jsonOut.String(), canary) {
					t.Fatalf("positive control failed: --json did not carry the sentinel for %q; the absence check below proves nothing", value)
				}

				humanOut := &bytes.Buffer{}
				if err := render(humanOut, env); err != nil {
					t.Fatalf("render error = %v", err)
				}
				if strings.Contains(humanOut.String(), canary) {
					t.Fatalf("sentinel reached the %s summary: %q", name, humanOut.String())
				}
			})
		}
	}
}

// envelopeWithHiddenSentinel builds an envelope whose rendered fields are all
// ordinary, with the sentinel only under members the summaries do not print.
func envelopeWithHiddenSentinel(value string) Envelope {
	return Envelope{
		Data: map[string]any{
			"count":                        float64(1),
			"scope_id":                     "scope-a",
			"service_id":                   "svc-a",
			"since_generation_id":          "gen-prior",
			"current_active_generation_id": "gen-current",
			// Not a rendered key.
			"operator_note": value,
			"generations": []any{map[string]any{
				"generation_id": "gen-new",
				"status":        "active",
				// Not a rendered key on a generation row.
				"request_context": value,
			}},
			"categories": []any{map[string]any{
				"category": "files",
				"counts":   map[string]any{"added": float64(1)},
				// Not a rendered key on a category row.
				"sample_handles": []any{value},
			}},
		},
		Truth: map[string]any{
			"freshness": map[string]any{"state": "fresh", "source": value},
			"selectors": map[string]any{"scope_id": value},
		},
	}
}

// TestRenderedErrorCarriesTheRequestURL pins the one carrier this family does
// not screen. When a request never reaches the API, net/http's error text
// embeds the request URL, which carries the --service-url endpoint and every
// selector the operator passed. The CLI prints that text verbatim, on stdout
// through this function and again on stderr through the exit path.
//
// The limit worth stating: net/http strips a URL's userinfo before this point,
// so a password embedded in --service-url does not appear here. Everything
// else in the URL does -- including a secret an operator typed into --scope-id.
// This test exists so that stays a known, asserted property rather than a
// surprise, and so a future change that starts screening the message has to
// change this test deliberately.
func TestRenderedErrorCarriesTheRequestURL(t *testing.T) {
	path := GenerationsPath(GenerationsOptions{ScopeID: "scope:" + canary, Limit: 50})
	if !strings.Contains(path, "CANARY-a1b2c3d4e5f6") {
		t.Fatalf("positive control failed: the selector did not reach the request path %q", path)
	}

	transportErr := errors.New(`request failed: Get "http://eshu.internal` + path + `": dial tcp: connection refused`)
	env := transportEnvelope(transportErr)
	if env.Error.Code != "backend_unavailable" {
		t.Fatalf("classified as %q, want backend_unavailable", env.Error.Code)
	}

	out := &bytes.Buffer{}
	if err := RenderEnvelopeError(out, env); err != nil {
		t.Fatalf("RenderEnvelopeError() error = %v", err)
	}
	if !strings.Contains(out.String(), canary) {
		t.Fatalf("the transport error stopped carrying the selector; update this test deliberately if that was the intent: %q", out.String())
	}
	if !strings.Contains(out.String(), "eshu.internal") {
		t.Fatalf("the transport error stopped carrying the endpoint: %q", out.String())
	}
}

// TestJSONBytesOnDiskMatchTheEnvelope runs the --json path through a real file
// rather than a bytes.Buffer, because the bytes an operator redirects to a file
// are what actually get shared. It asserts the same split: rendered-key values
// survive, and nothing is dropped or mangled on the way to disk.
func TestJSONBytesOnDiskMatchTheEnvelope(t *testing.T) {
	for carrier, value := range canaryCarriers() {
		t.Run(carrier, func(t *testing.T) {
			env := envelopeWithHiddenSentinel(value)
			target := filepath.Join(t.TempDir(), "envelope.json")

			file, err := os.Create(target) //nolint:gosec // the path is a test temp dir
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			writeErr := WriteJSON(file, env)
			closeErr := file.Close()
			if writeErr != nil {
				t.Fatalf("WriteJSON() error = %v", writeErr)
			}
			if closeErr != nil {
				t.Fatalf("close: %v", closeErr)
			}

			raw, err := os.ReadFile(target) //nolint:gosec // the path is a test temp dir
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !bytes.Contains(raw, []byte(value)) {
				t.Fatalf("the on-disk envelope lost the value %q:\n%s", value, raw)
			}

			var round Envelope
			if err := json.Unmarshal(raw, &round); err != nil {
				t.Fatalf("the on-disk envelope is not valid JSON: %v", err)
			}
			if got := stringValue(round.Data, "operator_note"); got != strings.TrimSpace(value) {
				t.Fatalf("round-tripped operator_note = %q, want %q", got, value)
			}
		})
	}
}

// TestRenderedSelectorsAreVerbatim covers the other direction for the keys the
// summaries do render: a scope, service, or generation id reaches the terminal
// exactly as the server echoed it, with no partial masking that would make an
// operator misread which scope they are looking at.
func TestRenderedSelectorsAreVerbatim(t *testing.T) {
	for carrier, value := range canaryCarriers() {
		t.Run(carrier, func(t *testing.T) {
			env := Envelope{Data: map[string]any{
				"count": float64(1),
				"generations": []any{map[string]any{
					"generation_id": value,
					"status":        "active",
					"scope_id":      value,
					"trigger_kind":  "push",
				}},
			}}
			out := &bytes.Buffer{}
			if err := RenderGenerationsSummary(out, env); err != nil {
				t.Fatalf("RenderGenerationsSummary() error = %v", err)
			}
			want := "  " + strings.TrimSpace(value) + " status=active scope=" + strings.TrimSpace(value) + " trigger=push\n"
			if !strings.Contains(out.String(), want) {
				t.Fatalf("summary %q did not render the row verbatim; want it to contain %q", out.String(), want)
			}
		})
	}
}
