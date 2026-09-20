// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/codedivergence"
)

// This file holds the DomainCodeDrifted durable-write registration gate
// tests added by issue #6837, split out of defaults_test.go (already near
// the repository's 500-line file cap) following the config-state drift
// gate pattern in defaults_config_state_drift_writer_gate_test.go.

// stubDriftedFindingWriter is a no-op codedivergence.FindingWriter used only
// to satisfy the non-nil registration gate.
type stubDriftedFindingWriter struct{}

func (stubDriftedFindingWriter) WriteDriftedFindings(
	_ context.Context, _ codedivergence.DriftedWrite,
) (codedivergence.DriftedWriteResult, error) {
	return codedivergence.DriftedWriteResult{}, nil
}

// stubDriftedCandidateLoader is a no-op codedivergence.CandidateLoader used
// only to satisfy the non-nil registration gate.
type stubDriftedCandidateLoader struct{}

func (stubDriftedCandidateLoader) LoadCandidates(
	_ context.Context, _ string,
) (codedivergence.CandidatePage, error) {
	return codedivergence.CandidatePage{}, nil
}

func TestImplementedDefaultDomainDefinitionsIncludesCodeDriftedWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		DriftHandlers: DriftHandlers{
			DriftedCandidateLoader: stubDriftedCandidateLoader{},
			DriftedFindingWriter:   &stubDriftedFindingWriter{},
			DriftedLogger:          logger,
		},
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainCodeDrifted {
			found = true
			if _, ok := def.Handler.(codedivergence.CodeDriftedHandler); !ok {
				t.Fatalf("code_drifted handler type = %T, want CodeDriftedHandler", def.Handler)
			}
		}
	}
	if !found {
		t.Fatal("code_drifted not registered after wiring loader+writer+logger")
	}
}

// TestImplementedDefaultDomainDefinitionsOmitsCodeDriftedWithoutWriter
// proves the durable-write gate: a loader present but no DriftedFindingWriter
// must still omit registration, so the reducer can never admit pairs with
// no durable truth surface.
func TestImplementedDefaultDomainDefinitionsOmitsCodeDriftedWithoutWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		DriftHandlers: DriftHandlers{
			DriftedCandidateLoader: stubDriftedCandidateLoader{},
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainCodeDrifted {
			t.Fatal("code_drifted registered without a DriftedFindingWriter; want omitted to avoid admitting pairs with no durable truth surface")
		}
	}
}

// TestImplementedDefaultDomainDefinitionsOmitsCodeDriftedWithoutLoader
// proves the observable-input gate: a writer present but no candidate
// loader must still omit registration, so the domain never registers with
// no evidence to verify.
func TestImplementedDefaultDomainDefinitionsOmitsCodeDriftedWithoutLoader(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		DriftHandlers: DriftHandlers{
			DriftedFindingWriter: &stubDriftedFindingWriter{},
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainCodeDrifted {
			t.Fatal("code_drifted registered without a DriftedCandidateLoader; want omitted to avoid registering with no observable input")
		}
	}
}
