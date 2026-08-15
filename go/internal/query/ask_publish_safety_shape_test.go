// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The Ask publish path calls answerguardrail.UnsafeString on three things it is
// about to hand a caller: the derived deterministic summary, each limitation,
// and each streamed token delta. The screen matched the literal substrings
// "http://" and "https://", so a connection string on any other scheme -- or on
// none -- went out over the wire.
//
// The synthetic secret below is a sentinel, not a credential. Any assertion
// that looks for it in a response body is asking "did the product publish the
// thing it promised to screen".

const (
	// askShapeSentinel is the value that must never reach a response body.
	askShapeSentinel = "S3NT1NEL"
	// askShapeUnscreenedDSN is the carrier origin/main published clean.
	askShapeUnscreenedDSN = "bolt://neo4j:" + askShapeSentinel + "@graph.example.com:7687"
	// askShapeAlreadyScreened is the positive control: a carrier origin/main
	// already refused, in the same slot. Every assertion below runs against it
	// too, so a test reporting the new carrier screened has first shown it can
	// observe screening at all.
	askShapeAlreadyScreened = "http://graph.example.com:7687"
)

// askShapeCarriers pairs each carrier with a name for the subtest.
var askShapeCarriers = map[string]string{
	"already_screened_control": askShapeAlreadyScreened,
	"bolt_dsn":                 askShapeUnscreenedDSN,
	"postgres_dsn":             "postgres://eshu:" + askShapeSentinel + "@db.example.net:5432/eshu",
	"schemeless_dsn":           "svc:" + askShapeSentinel + "@host/tool",
	"bracketed_ipv6":           "[fd00::1]:7687",
	"bare_ipv6":                "fd00::1",
	"password_colon":           "password: " + askShapeSentinel,
}

// supportedPacketWithSummary builds the one shape that reaches
// applyDerivedProseFallback: a supported packet with a deterministic Summary
// and no governed narration.
func supportedPacketWithSummary(summary string) AskAnswer {
	return AskAnswer{
		Narrated: false,
		Packets: []AnswerPacket{{
			PromptFamily: "service_story",
			TruthClass:   AnswerTruthDeterministic,
			Summary:      summary,
			Supported:    true,
		}},
		Trace: []AskTraceEntry{
			{Tool: "get_service_story", Supported: true, TruthClass: AnswerTruthDeterministic},
		},
	}
}

// TestAskDoesNotPublishADerivedSummaryCarryingACredentialOrAddress drives the
// real POST /api/v0/ask handler. It is the live publish path: only the ask
// engine is stubbed.
func TestAskDoesNotPublishADerivedSummaryCarryingACredentialOrAddress(t *testing.T) {
	t.Parallel()

	for name, carrier := range askShapeCarriers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			summary := "The graph backend is reachable at " + carrier + " for this service."
			h := &AskHandler{Asker: &fakeAsker{answer: supportedPacketWithSummary(summary)}}
			w := postAsk(h, `{"question":"where does the graph backend live?"}`)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), carrier) {
				t.Fatalf("response body published %q; the publish-safety screen let it through", carrier)
			}

			var resp askResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if resp.AnswerProse != "" {
				t.Fatalf("answer_prose = %q, want withheld", resp.AnswerProse)
			}
			if !hasAskLimitationContaining(resp.Limitations, "failed publish-safety scan") {
				t.Fatalf("limitations = %v, want the publish-safety withholding stated", resp.Limitations)
			}
		})
	}
}

// TestAskPublishesAnHonestDerivedSummary is the negative control for the test
// above. Without it, a screen that rejected everything would look like a pass.
func TestAskPublishesAnHonestDerivedSummary(t *testing.T) {
	t.Parallel()

	const summary = "checkout-api is deployed to production via Helm from repo:demo/service."
	h := &AskHandler{Asker: &fakeAsker{answer: supportedPacketWithSummary(summary)}}
	w := postAsk(h, `{"question":"where is checkout-api deployed?"}`)

	var resp askResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.AnswerProse != summary {
		t.Fatalf("answer_prose = %q, want the honest summary published; the widened screen is over-rejecting", resp.AnswerProse)
	}
}

// TestAskScrubsALimitationCarryingACredentialOrAddress covers the second
// caller: publishSafeAskLimitations drops an unsafe limitation when the
// publish-safety criterion fires.
func TestAskScrubsALimitationCarryingACredentialOrAddress(t *testing.T) {
	t.Parallel()

	for name, carrier := range askShapeCarriers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			unsafe := "graph backend unreachable at " + carrier
			ans := AskAnswer{
				// Narrated prose that is itself unsafe, so the runtime guardrail's
				// publish-safety criterion fires and the limitation scrub runs.
				Prose:       "The backend at " + carrier + " is down.",
				Narrated:    true,
				Partial:     true,
				Limitations: []string{unsafe, "results limited to 100 rows"},
				Packets: []AnswerPacket{{
					TruthClass: AnswerTruthDeterministic,
					Supported:  true,
				}},
			}
			h := &AskHandler{Asker: &fakeAsker{answer: ans}}
			w := postAsk(h, `{"question":"is the graph backend up?"}`)

			if strings.Contains(w.Body.String(), carrier) {
				t.Fatalf("response body published %q through a limitation", carrier)
			}

			var resp askResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if !hasAskLimitationContaining(resp.Limitations, "results limited to 100 rows") {
				t.Fatalf("limitations = %v, want the safe limitation kept", resp.Limitations)
			}
		})
	}
}

// TestAskStreamTokenDeltasAreSafeRejectsEveryCarrier covers the third caller.
// The SSE path checks each delta and their concatenation, so a carrier split
// across two tokens must still be caught.
func TestAskStreamTokenDeltasAreSafeRejectsEveryCarrier(t *testing.T) {
	t.Parallel()

	if !askStreamTokenDeltasAreSafe([]string{"checkout-api ", "is deployed to production."}) {
		t.Fatal("honest token deltas reported unsafe; every rejection below would be meaningless")
	}

	for name, carrier := range askShapeCarriers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if askStreamTokenDeltasAreSafe([]string{"reachable at ", carrier}) {
				t.Fatalf("token deltas carrying %q reported safe", carrier)
			}
			// Split down the middle so no single delta carries the whole value.
			mid := len(carrier) / 2
			if askStreamTokenDeltasAreSafe([]string{"reachable at " + carrier[:mid], carrier[mid:]}) {
				t.Fatalf("token deltas carrying %q across a split reported safe", carrier)
			}
		})
	}
}

// hasAskLimitationContaining reports whether any limitation contains want.
func hasAskLimitationContaining(limitations []string, want string) bool {
	for _, limitation := range limitations {
		if strings.Contains(limitation, want) {
			return true
		}
	}
	return false
}
