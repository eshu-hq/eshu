// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"testing"
)

// Wave-5 rider seven-leg replication, leg 5: parity-POST (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a parity-POST legs: the R1 wrong-method judgment
// is byte-identical across lanes, bodies, and envelopes — the request
// body never changes the judgment, and Allow always names the pinned
// POST method. The upstream-body equality half is vacuous for a
// judgment (no upstream body crosses: the mux answers before any
// reader runs), which the byte-identity assertions below prove rather
// than assume.

// w5RidersParityPostBodies is the body matrix the R1 judgment must
// ignore: empty, the smallest valid bundle probe, an oversized-shape
// probe, and a non-JSON probe.
var w5RidersParityPostBodies = []string{
	"",
	`{}`,
	`{"anchor":{"repo_id":"r","entity_id":"e"},"limit":5}`,
	`not-json`,
}

// TestW5RidersPostJudgmentsByteIdentical pins that two identically
// built handler lanes judge R1 byte-identically across the body matrix
// and both envelopes: same 405 status, same Allow, same body.
func TestW5RidersPostJudgmentsByteIdentical(t *testing.T) {
	t.Parallel()

	for _, body := range w5RidersParityPostBodies {
		for _, accept := range []string{EnvelopeMIMEType, "application/json"} {
			name := "PATCH body=" + body + " " + accept
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				laneA, _, _ := w5RidersCannedMux()
				laneB, _, _ := w5RidersCannedMux()
				statusA, headerA, respA := w5RidersServeOnce(laneA, http.MethodPatch, "/api/v0/code/bundles", accept, body)
				statusB, headerB, respB := w5RidersServeOnce(laneB, http.MethodPatch, "/api/v0/code/bundles", accept, body)

				if statusA != http.StatusMethodNotAllowed || statusB != http.StatusMethodNotAllowed {
					t.Fatalf("lane status = %d vs %d, want identical 405", statusA, statusB)
				}
				if headerA.Get("Allow") != headerB.Get("Allow") {
					t.Errorf("Allow = %q vs %q, want identical", headerA.Get("Allow"), headerB.Get("Allow"))
				}
				if !strings.Contains(headerA.Get("Allow"), http.MethodPost) {
					t.Errorf("Allow = %q, want it to name the pinned POST method", headerA.Get("Allow"))
				}
				if string(respA) != string(respB) {
					t.Errorf("lane body mismatch:\nA=%s\nB=%s", respA, respB)
				}
			})
		}
	}
}

// TestW5RidersBodiesNeverChangeJudgment pins that no request body
// turns the R1 judgment into service: every body in the matrix gets
// the same 405, and a GET on the bundles path (a second wrong-method
// spelling of the same mounted POST leg) is judged 405 like R1 rather
// than borrowing the POST leg.
func TestW5RidersBodiesNeverChangeJudgment(t *testing.T) {
	t.Parallel()

	mux, policy, providers := w5RidersCannedMux()
	before := policy.contacts() + providers.contacts()
	for _, body := range w5RidersParityPostBodies {
		status, _, _ := w5RidersServeOnce(mux, http.MethodPatch, "/api/v0/code/bundles", EnvelopeMIMEType, body)
		if status != http.StatusMethodNotAllowed {
			t.Errorf("PATCH bundles body=%q = %d, want 405", body, status)
		}
	}
	if status, header, _ := w5RidersServeOnce(mux, http.MethodGet, "/api/v0/code/bundles", EnvelopeMIMEType, ""); status != http.StatusMethodNotAllowed {
		t.Errorf("GET bundles = %d, want 405 (the POST leg is mounted; a GET spelling is judged like R1)", status)
	} else if allow := header.Get("Allow"); !strings.Contains(allow, http.MethodPost) {
		t.Errorf("GET bundles Allow = %q, want it to name the pinned POST method", allow)
	}
	if got := policy.contacts() + providers.contacts() - before; got != 0 {
		t.Errorf("judgment probes contacted admissions %d times, want zero contact", got)
	}
}
