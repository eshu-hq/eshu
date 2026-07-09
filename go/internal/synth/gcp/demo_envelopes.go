// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// DemoOrgFactEnvelopes generates the demo-org GCP cassette for profile and
// replays it through the production cassette.Source seam
// (go/internal/replay/cassette/source.go) — the same collector.Source
// implementation collector.Service drives against a real cassette file —
// returning every fact envelope the generated cassette's scopes carry.
//
// This is deliberately not a hand-built mirror of the generator's payload
// shapes: routing through GenerateDemoOrgCassette -> cassette.ParseAndValidate
// -> cassette.Source proves the envelopes a consumer (Ifá's demo-org
// round-trip Odù, go/internal/ifa/roundtrip.go) exercises are byte-faithful to
// what the real replay path would emit for this cassette, not a
// hand-maintained approximation of it that could silently drift from the
// generator or the cassette format.
func DemoOrgFactEnvelopes(profile DemoOrgProfile) ([]facts.Envelope, error) {
	generated, err := GenerateDemoOrgCassette(profile)
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: generate demo-org cassette: %w", err)
	}

	file, err := cassette.ParseAndValidate(generated.Bytes)
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: parse generated demo-org cassette: %w", err)
	}

	src := &cassette.Source{File: file}
	ctx := context.Background()

	var envelopes []facts.Envelope
	for {
		gen, ok, err := src.Next(ctx)
		if err != nil {
			return nil, fmt.Errorf("synth/gcp: replay demo-org cassette: %w", err)
		}
		if !ok {
			// Source.Next returns ok=false once every scope in the cassette has
			// been emitted, then rearms for a subsequent poll (see Source.Next's
			// doc comment). A single drain pass over a fixed, in-memory cassette
			// visits every scope exactly once before this happens, so it is safe
			// to stop here rather than looping forever.
			break
		}
		for env := range gen.Facts {
			envelopes = append(envelopes, env)
		}
	}
	return envelopes, nil
}
