// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codefunctionsummary

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildCodeFunctionSummaryReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()
	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: "file"}})
	if _, ok := BuildCodeFunctionSummaryReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a summary intent without any code_function_summary fact")
	}
}

func TestBuildCodeFunctionSummaryReducerIntentFromFact(t *testing.T) {
	t.Parallel()
	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{
			FactKind:      facts.CodeFunctionSummaryFactKind,
			FactID:        "summary-fact-1",
			CollectorKind: "git",
			Payload:       map[string]any{"function_id": "repo-1\x1fpkg\x1f\x1fHandle"},
		},
	})
	intent, ok := BuildCodeFunctionSummaryReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_function_summary fact")
	}
	if intent.Domain != reducer.DomainCodeFunctionSummary || intent.EntityKey != "code_function_summary:scope-1" {
		t.Fatalf("intent domain/key wrong: %+v", intent)
	}
	if intent.FactID != "summary-fact-1" || intent.SourceSystem != "git" {
		t.Fatalf("intent fact/source not carried: %+v", intent)
	}
	if intent.Payload["repo_id"] != "repo-1" {
		t.Fatalf("intent payload = %#v, want repo_id", intent.Payload)
	}
	if _, ok := intent.Payload["full_snapshot"]; ok {
		t.Fatalf("summary-only intent marked full snapshot: %#v", intent.Payload)
	}
}

func TestBuildCodeFunctionSummaryReducerIntentSkipsInvalidSummaryRepoID(t *testing.T) {
	t.Parallel()
	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{
			FactKind:      facts.CodeFunctionSummaryFactKind,
			FactID:        "summary-fact-1",
			CollectorKind: "git",
			Payload:       map[string]any{"repo_id": "repo-1"},
		},
	})
	intent, ok := BuildCodeFunctionSummaryReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_function_summary fact")
	}
	if _, ok := intent.Payload["repo_id"]; ok {
		t.Fatalf("intent payload = %#v, want no repo_id from input_invalid summary", intent.Payload)
	}
}

func TestBuildCodeFunctionSummaryReducerIntentFromMarkerOnly(t *testing.T) {
	t.Parallel()
	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{
			FactKind:      facts.CodeDataflowScannedFactKind,
			FactID:        "marker-1",
			CollectorKind: "git",
			Payload:       map[string]any{"repo_id": "repo-1"},
		},
	})
	intent, ok := BuildCodeFunctionSummaryReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for marker-only full dataflow scan")
	}
	if intent.FactID != "marker-1" || intent.Reason != "value-flow gate scanned; reconcile function summaries" {
		t.Fatalf("intent trigger = %+v, want marker provenance", intent)
	}
	if intent.Payload["repo_id"] != "repo-1" || intent.Payload["full_snapshot"] != true {
		t.Fatalf("intent payload = %#v, want full repo snapshot marker", intent.Payload)
	}
}
