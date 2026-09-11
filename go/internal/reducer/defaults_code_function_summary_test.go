// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// The function-summary family's production code and its own handler tests
// moved to internal/reducer/code/function/summary under issue #6061. These
// two wiring tests stay in root because they exercise NewDefaultRegistry and
// implementedDefaultDomainDefinitions, the composition root that only this
// package may hold. wiringSummaryLoader and wiringSummaryWriter are trivial
// local stand-ins satisfying CodeFunctionSummaryLoader/CodeFunctionSummaryWriter
// (aliases for summary.Loader/summary.Writer): the full-featured test doubles
// that build real envelopes/snapshots moved with the rest of the family.

// wiringSummaryLoader satisfies CodeFunctionSummaryLoader with no facts.
type wiringSummaryLoader struct{}

func (wiringSummaryLoader) LoadCodeFunctionSummaryFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return nil, nil
}

// wiringSummaryWriter satisfies CodeFunctionSummaryWriter as a no-op.
type wiringSummaryWriter struct{}

func (wiringSummaryWriter) LoadSnapshot(context.Context) (summary.Snapshot, error) {
	return summary.Snapshot{}, nil
}

func (wiringSummaryWriter) UpsertSnapshot(context.Context, summary.Snapshot, time.Time) error {
	return nil
}

func (wiringSummaryWriter) ReplaceSnapshot(context.Context, string, summary.Snapshot, time.Time) error {
	return nil
}

func TestImplementedDefaultDomainDefinitionsOmitsCodeFunctionSummaryWithoutWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeFunctionSummaryLoader: wiringSummaryLoader{},
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainCodeFunctionSummary {
			t.Fatalf("code_function_summary registered without writer; want omitted")
		}
	}
}

func TestNewDefaultRegistryAcceptsCodeFunctionSummaryWhenWired(t *testing.T) {
	t.Parallel()

	registry, err := NewDefaultRegistry(DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeFunctionSummaryLoader: wiringSummaryLoader{},
			CodeFunctionSummaryWriter: wiringSummaryWriter{},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry returned error with summary wired: %v", err)
	}
	def, ok := registry.Definition(DomainCodeFunctionSummary)
	if !ok {
		t.Fatal("code_function_summary not registered when wired")
	}
	if _, ok := def.Handler.(CodeFunctionSummaryMaterializationHandler); !ok {
		t.Fatalf("handler type = %T, want CodeFunctionSummaryMaterializationHandler", def.Handler)
	}
}
