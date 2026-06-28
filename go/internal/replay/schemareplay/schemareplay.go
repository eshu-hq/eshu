// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemareplay

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// AdmissionResult is the outcome of replaying one recorded fact through the
// current schema-version admission gate. Admitted reports whether the production
// admission function accepted the fact; Err carries the explicit refusal when it
// did not. A fact is never silently admitted under the wrong interpretation: it
// is either Admitted with a nil Err, or refused with a non-nil Err.
type AdmissionResult struct {
	FactKind      string
	SchemaVersion string
	StableFactKey string
	Admitted      bool
	Err           error
}

// ReplayAdmission loads a frozen cassette and drives every recorded fact through
// the REAL production admission function (facts.ValidateSchemaVersion — the same
// function the projector wires as the per-fact AdmissionHook) without any graph
// backend or Postgres. It returns one AdmissionResult per fact, in cassette
// order, so a test can assert each historical-version fact's defined outcome
// against the current code.
//
// It fails loudly on a malformed cassette or a fact-stream error rather than
// returning a short result set that would look green.
func ReplayAdmission(cassettePath string) ([]AdmissionResult, error) {
	src, err := cassette.NewSource(cassettePath)
	if err != nil {
		return nil, fmt.Errorf("open cassette %q: %w", cassettePath, err)
	}

	var results []AdmissionResult
	for {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			return nil, fmt.Errorf("read cassette generation: %w", err)
		}
		if !ok {
			break
		}
		for env := range gen.Facts {
			admitErr := facts.ValidateSchemaVersion(env.FactKind, env.SchemaVersion)
			results = append(results, AdmissionResult{
				FactKind:      env.FactKind,
				SchemaVersion: env.SchemaVersion,
				StableFactKey: env.StableFactKey,
				Admitted:      admitErr == nil,
				Err:           admitErr,
			})
		}
		if gen.FactStreamErr != nil {
			if streamErr := gen.FactStreamErr(); streamErr != nil {
				return nil, fmt.Errorf("fact stream error: %w", streamErr)
			}
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("cassette %q yielded no facts", cassettePath)
	}
	return results, nil
}
