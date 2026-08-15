// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitymap

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// This file answers one question about the map family: can anything the
// package holds, other than the API's own response, end up in what an operator
// sees?
//
// Every non-literal string the package prints comes from one of two places:
// the response (data.from, resolution.selected/candidates, sections.*,
// evidence.relationship_count, truth.freshness.state, error.message/code) or
// the transport error's own text. Nothing else is composed -- the package
// never reads a flag, a config value, a credential, or a file path, and it
// writes only to the io.Writer its caller supplies.
//
// So the credential below is deliberately unreachable: it is held by the fake
// client, which is the only thing in these tests that has one. The tests
// assert it never appears in rendered output, and
// TestCredentialCanaryFiresWhenTheResponseEchoesIt proves that assertion can
// fail, by making the fake echo the credential back inside a response value.

const (
	// canaryCredential stands in for an API key an operator configured. It is
	// held only by the fake client.
	canaryCredential = "eshu-sk-canary-7f3a2b41"
	// canarySentinel is planted inside response values to prove the checks
	// below read the real rendered bytes rather than an empty buffer.
	canarySentinel = "CANARY9d4e"
)

// canaryCarriers place the sentinel inside a value, varying the character
// immediately before it: a JSON encoder, a %s verb, or a screen that anchors
// on word boundaries can each behave differently depending on what precedes
// the run of characters it is looking at.
var canaryCarriers = []struct {
	name  string
	value string
}{
	{"segment start", "workload:" + canarySentinel},
	{"letter", "orders" + canarySentinel},
	{"space", "orders " + canarySentinel},
	{"at sign", "orders@" + canarySentinel},
	{"double quote", `orders"` + canarySentinel},
	{"colon", "orders:" + canarySentinel},
	{"dot", "orders." + canarySentinel},
	{"dash", "orders-" + canarySentinel},
}

// credentialHoldingPoster is the only value in these tests that knows the
// credential. It mirrors go/cmd/eshu's *APIClient, which holds an API key and
// sends it as a header; the key is never part of a response body.
type credentialHoldingPoster struct {
	credential string
	// echoCredential makes the fake write its credential into a response
	// value. It exists to prove the absence checks below can fail.
	echoCredential bool
	carrier        string
}

func (p *credentialHoldingPoster) PostEnvelope(_ string, _, result any) error {
	selector := p.carrier
	if p.echoCredential {
		selector = p.carrier + p.credential
	}
	envelope, ok := result.(*Envelope)
	if !ok {
		return nil
	}
	*envelope = Envelope{
		Data: map[string]any{
			"status": "mapped",
			"from":   selector,
			"resolution": map[string]any{
				"selected": map[string]any{"id": selector, "name": selector, "labels": []any{selector}},
			},
			"sections": map[string]any{
				"depends_on": []any{
					map[string]any{"relationship_type": "DEPENDS_ON", "entity_name": selector, "repo_id": selector},
				},
			},
			"evidence": map[string]any{"relationship_count": float64(1)},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}
	return nil
}

// renderCarrier runs the full production path -- Fetch, Resolve, Write -- for
// one carrier in both output modes, and returns the bytes an operator would
// see. Both the absence check and its control call this, so they exercise the
// same code.
func renderCarrier(t *testing.T, poster *credentialHoldingPoster, carrier string) string {
	t.Helper()
	poster.carrier = carrier
	var combined strings.Builder
	for _, jsonOutput := range []bool{false, true} {
		out := &bytes.Buffer{}
		envelope, failure := Resolve(Fetch(poster, Options{From: carrier, JSON: jsonOutput}))
		if err := Write(out, jsonOutput, envelope, failure); err != nil {
			t.Fatalf("Write(json=%t) error = %v, want nil", jsonOutput, err)
		}
		if out.Len() == 0 {
			t.Fatalf("Write(json=%t) produced no output; the check below would pass vacuously", jsonOutput)
		}
		combined.WriteString(out.String())
	}
	return combined.String()
}

func TestRenderedOutputNeverCarriesTheClientCredential(t *testing.T) {
	for _, carrier := range canaryCarriers {
		t.Run(carrier.name, func(t *testing.T) {
			poster := &credentialHoldingPoster{credential: canaryCredential}
			output := renderCarrier(t, poster, carrier.value)

			// Positive control: the sentinel the response carried MUST be in
			// the output. Without this, a check that found no credential
			// would prove nothing about whether it looked at anything.
			if !strings.Contains(output, canarySentinel) {
				t.Fatalf("sentinel %q missing from rendered output; the absence check below would be vacuous:\n%s", canarySentinel, output)
			}
			if strings.Contains(output, canaryCredential) {
				t.Fatalf("credential %q reached rendered output:\n%s", canaryCredential, output)
			}
		})
	}
}

// TestCredentialCanaryFiresWhenTheResponseEchoesIt is the control run for the
// test above. It drives the same renderCarrier path with a fake that echoes
// its credential into a response value, and asserts the credential IS found --
// so a clean result from the test above is a result from a check that can
// fail.
func TestCredentialCanaryFiresWhenTheResponseEchoesIt(t *testing.T) {
	for _, carrier := range canaryCarriers {
		t.Run(carrier.name, func(t *testing.T) {
			poster := &credentialHoldingPoster{credential: canaryCredential, echoCredential: true}
			output := renderCarrier(t, poster, carrier.value)

			if !strings.Contains(output, canaryCredential) {
				t.Fatalf("planted credential %q was NOT found, so the absence check cannot fail:\n%s", canaryCredential, output)
			}
		})
	}
}

// TestRenderingWritesNothingToDisk pins the "bytes on disk" half: the package
// takes an io.Writer and has no file path, so a rendered map must leave the
// filesystem untouched.
func TestRenderingWritesNothingToDisk(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	poster := &credentialHoldingPoster{credential: canaryCredential}
	_ = renderCarrier(t, poster, "workload:"+canarySentinel)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v, want nil", err)
	}
	if len(entries) != 0 {
		t.Fatalf("rendering created %d filesystem entries, want none: %v", len(entries), entries)
	}
}
