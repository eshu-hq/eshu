// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

// Knobs for the #11142 M6 narrow-store write proof. This file opens no proof
// database, so it may read the environment: the enrollment guard forbids
// direct env reads only in files that open a proof database, and the proof
// files below reach the DSN exclusively through newKeyedHarness.

import (
	"os"
	"testing"
)

// narrowProofEnv selects the write-proof corpus. Unset or "smoke" runs a
// small QA-shaped corpus so the proof stays a fast gate test. "full" runs the
// larger corpus on a quiet host.
const narrowProofEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_NARROW_PROOF"

// narrowProofCorpus is the generated corpus shape: scopes of findingsPerScope
// each, written through the production impact writer so every payload is
// QA-shaped (same encoder, same columns, same production index set).
type narrowProofCorpus struct {
	label            string
	scopes           int
	findingsPerScope int
}

func narrowProofCorpusFor(t *testing.T) narrowProofCorpus {
	t.Helper()
	multiple := map[string]narrowProofCorpus{
		"":      {label: "smoke", scopes: 4, findingsPerScope: 250},
		"smoke": {label: "smoke", scopes: 4, findingsPerScope: 250},
		"full":  {label: "full", scopes: 40, findingsPerScope: 1000},
	}
	corpus, ok := multiple[os.Getenv(narrowProofEnv)]
	if !ok {
		t.Fatalf("%s=%q: want smoke or full", narrowProofEnv, os.Getenv(narrowProofEnv))
	}
	return corpus
}

// narrowProofWriters returns the writer counts the write bar measures.
func narrowProofWriters() []int { return []int{8, 16} }
