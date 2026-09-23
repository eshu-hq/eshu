// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestClassifyAnswerTruthPinsNoBackendReadToFallback pins the
// TruthBasisNoBackendRead arm of ClassifyAnswerTruth directly, with a Level
// that the other arms would otherwise classify as a real answer.
//
// #6544 already covers the arm from root, through the forwarder:
// TestAnswerPacketTruthClassMapping/no_backend_read_never_upgrades fails
// without it (verified by deleting the arm). This test covers the same rule at
// the answer entry point directly, so the arm stays pinned even if root
// stops forwarding to it. Constructing the envelope literally is deliberate:
// BuildTruthEnvelope cannot reach this state, because basisLevel already forces
// a no-read page to TruthLevelFallback.
func TestClassifyAnswerTruthPinsNoBackendReadToFallback(t *testing.T) {
	for _, level := range []querycontract.TruthLevel{querycontract.TruthLevelExact, querycontract.TruthLevelDerived} {
		got := ClassifyAnswerTruth(&querycontract.TruthEnvelope{
			Basis: querycontract.TruthBasisNoBackendRead,
			Level: level,
		})
		if got != querycontract.AnswerTruthFallback {
			t.Errorf("ClassifyAnswerTruth(basis=%s, level=%s) = %s, want %s; a page produced without reading any backend must never classify as a real answer",
				querycontract.TruthBasisNoBackendRead, level, got, querycontract.AnswerTruthFallback)
		}
	}
}
