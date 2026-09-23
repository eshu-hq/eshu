// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// The regressions for the third review round of #6987. Every literal is
// synthetic: the account is the gate's own planted sample and the names
// are invented.

const (
	hexName    = `n[0-9a-f]{11}`
	pseudoAcct = `0000[0-9]{8}`
)

// wrapGens drains a wrapped source and returns the rewritten generations
// with their envelopes materialized, plus the report.
func wrapGens(t *testing.T, src collector.Source, key recordpseudo.Key, policy recordpseudo.Policy) ([]collector.CollectedGeneration, [][]facts.Envelope, *recordpseudo.Report) {
	t.Helper()
	wrapped, report, err := recordpseudo.Wrap(src, key, policy)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	var gens []collector.CollectedGeneration
	var envs [][]facts.Envelope
	for {
		gen, ok, err := wrapped.Next(context.Background())
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if !ok {
			return gens, envs, report
		}
		var out []facts.Envelope
		for env := range gen.Facts {
			out = append(out, env)
		}
		gens = append(gens, gen)
		envs = append(envs, out)
	}
}

func mustMatch(t *testing.T, label, got, want string) {
	t.Helper()
	if !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("%s: shape %q does not match %s", label, shapeOf(got), want)
	}
}

// withStableKey rebuilds a single-fact generation with the given stable key.
func withStableKey(gen collector.CollectedGeneration, stableKey string) collector.CollectedGeneration {
	var envs []facts.Envelope
	for env := range gen.Facts {
		env.StableFactKey = stableKey
		envs = append(envs, env)
	}
	return collector.FactsFromSlice(gen.Scope, gen.Generation, envs)
}
