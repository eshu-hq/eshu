// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"testing"
)

// G2 Wave-1 seven-leg replication, leg 5: parity-POST (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a POST parity legs: the 4 POST rider spellings
// are judged 405 before any admission runs, so parity is the
// judgment's own byte identity — the same rider POST answered by
// two identically built lanes is byte-identical across bodies and
// envelopes, and different caller bodies never change the judgment
// (the method is judged before the body is read). The family wire
// shape rides the Allow header, which names the pinned method per
// rider. Reference only — every line here is new core-side code.

// g2Wave1ParityPostBodies are the rider POST bodies the legs drive:
// an empty object and a filter-shaped body. Both must judge
// identically — the body never reaches a reader.
var g2Wave1ParityPostBodies = []string{
	`{}`,
	`{"scope_id":"scope_1","limit":50}`,
}

// g2Wave1RiderPaths are the 4 forwarded-read rider spellings.
var g2Wave1RiderPaths = []string{
	"/api/v0/freshness/changed-since",
	"/api/v0/freshness/generations",
	"/api/v0/freshness/services/changed-since",
	"/api/v0/surface-inventory",
}

// TestG2Wave1PostJudgmentsByteIdentical pins that two identically
// built lanes judge every rider POST byte-identically across bodies
// and envelopes: same 405, same Allow, same body.
func TestG2Wave1PostJudgmentsByteIdentical(t *testing.T) {
	t.Parallel()

	for _, path := range g2Wave1RiderPaths {
		for _, body := range g2Wave1ParityPostBodies {
			for _, accept := range []string{EnvelopeMIMEType, "application/json"} {
				name := "POST " + path + " " + accept
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					laneA, laneB := g2Wave1CannedMux(), g2Wave1CannedMux()
					statusA, headerA, bodyA := g2Wave1ServeOnce(laneA, http.MethodPost, path, accept, body)
					statusB, headerB, bodyB := g2Wave1ServeOnce(laneB, http.MethodPost, path, accept, body)

					if statusA != http.StatusMethodNotAllowed || statusB != http.StatusMethodNotAllowed {
						t.Fatalf("rider status = %d vs %d, want 405 on both lanes", statusA, statusB)
					}
					if headerA.Get("Allow") != headerB.Get("Allow") {
						t.Errorf("Allow = %q vs %q, want identical", headerA.Get("Allow"), headerB.Get("Allow"))
					}
					if string(bodyA) != string(bodyB) {
						t.Errorf("rider judgment body mismatch:\nA=%s\nB=%s", bodyA, bodyB)
					}
				})
			}
		}
	}
}

// TestG2Wave1PostBodiesNeverChangeTheJudgment pins that the caller
// body is irrelevant to the judgment: two different bodies on the
// same rider spell the byte-identical 405.
func TestG2Wave1PostBodiesNeverChangeTheJudgment(t *testing.T) {
	t.Parallel()

	for _, path := range g2Wave1RiderPaths {
		t.Run("POST "+path, func(t *testing.T) {
			t.Parallel()

			mux := g2Wave1CannedMux()
			statusA, _, bodyA := g2Wave1ServeOnce(mux, http.MethodPost, path, "application/json", g2Wave1ParityPostBodies[0])
			statusB, _, bodyB := g2Wave1ServeOnce(mux, http.MethodPost, path, "application/json", g2Wave1ParityPostBodies[1])
			if statusA != statusB {
				t.Fatalf("rider status = %d vs %d for different bodies, want identical", statusA, statusB)
			}
			if string(bodyA) != string(bodyB) {
				t.Errorf("rider judgment differs by body:\nA=%s\nB=%s", bodyA, bodyB)
			}
		})
	}
}

// TestG2Wave1PostAllowNamesThePinnedMethod pins the family wire
// shape of each rider judgment: the Allow header names GET, the
// pinned method of the base leg the rider twins.
func TestG2Wave1PostAllowNamesThePinnedMethod(t *testing.T) {
	t.Parallel()

	for _, path := range g2Wave1RiderPaths {
		t.Run("POST "+path, func(t *testing.T) {
			t.Parallel()

			_, header, _ := g2Wave1ServeOnce(g2Wave1CannedMux(), http.MethodPost, path, "application/json", `{}`)
			if allow := header.Get("Allow"); !strings.Contains(allow, http.MethodGet) {
				t.Errorf("POST %s: Allow = %q, want it to name the pinned GET method", path, allow)
			}
		})
	}
}
