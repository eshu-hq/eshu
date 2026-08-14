// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// endpointDifferentialMarker is the text cmd/eshu writes over a removed value
// (evidenceRedactedMarker). It carries no separator, so it cannot change where
// a boundary is found.
const endpointDifferentialMarker = "redacted"

// TestRedactionWalksAgreeOnTheSharedDifferential compares the two redaction
// walks to EACH OTHER over a generated cross-product, instead of comparing each
// to a hand-written expectation.
//
// Why this exists. The boundary corpus pins what each walk emits, and both walks
// passed every row of it while disagreeing on 81 of 378 generated inputs. Depth
// and position are decided in two places — urlredact.Query's own scan and this
// package's noteEscapedTerminators — and a shared table of expectations cannot
// catch the two drifting apart, because whoever writes a row already knows which
// case they are writing. Two walks deciding the same question independently is
// how this drifted twice; a differential makes the next drift a red test.
//
// The endpoint side runs urlredact.Query rather than cmd/eshu's redactEndpoint,
// because package main is not importable. Query is where the endpoint walk
// decides every boundary in this table — redactEndpoint adds URL parsing, the
// userinfo rule and the fragment rule around it, none of which touch a query
// boundary — and cmd/eshu's own
// TestRedactEndpointDelegatesTheQueryWalkForTheDifferential drives redactEndpoint
// through the identical rows and fails if that delegation stops holding. So the
// limit is real but bounded, and it is pinned rather than assumed.
func TestRedactionWalksAgreeOnTheSharedDifferential(t *testing.T) {
	t.Parallel()

	cases := urlredact.DifferentialCases()
	if len(cases) == 0 {
		t.Fatal("the differential is empty, so this test would pass vacuously")
	}
	t.Logf("comparing both walks over %d generated inputs", len(cases))

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			endpoint := urlredact.Query(tc.Input, endpointDifferentialMarker)
			freeText, _ := redactFreeText(tc.Input)
			if err := tc.CheckAgreement(endpoint, freeText); err != nil {
				t.Fatalf("input %q: %v", tc.Input, err)
			}
		})
	}
}
