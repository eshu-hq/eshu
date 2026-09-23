// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"context"
	"regexp"
	"strings"
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
	return withRefs(gen, stableKey, "", "")
}

// withRefs rebuilds a generation with the given composite envelope fields;
// an empty value keeps the field the generation already carries.
func withRefs(gen collector.CollectedGeneration, stableKey, sourceURI, recordID string) collector.CollectedGeneration {
	var envs []facts.Envelope
	for env := range gen.Facts {
		if stableKey != "" {
			env.StableFactKey = stableKey
		}
		if sourceURI != "" {
			env.SourceRef.SourceURI = sourceURI
		}
		if recordID != "" {
			env.SourceRef.SourceRecordID = recordID
		}
		envs = append(envs, env)
	}
	return collector.FactsFromSlice(gen.Scope, gen.Generation, envs)
}

// components splits a composite on "/" and ":" so a test can ask whether a
// raw token survives as a whole component.
func components(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == ':' })
}

func hasComponent(s, raw string) bool {
	for _, c := range components(s) {
		if c == raw {
			return true
		}
	}
	return false
}
