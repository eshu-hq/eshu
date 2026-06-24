// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestInheritanceMaterializationHandlerUsesPayloadFilteredContentEntities(t *testing.T) {
	t.Parallel()

	loader := &recordingPayloadFactLoader{
		byPayload: inheritanceEntityContentFacts(),
		all:       []facts.Envelope{{FactKind: "file"}},
		byKind: []facts.Envelope{{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"path":          "/repo",
				"source_run_id": "run-1",
			},
		}},
	}
	writer := &recordingInheritanceIntentWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-inheritance-payload-filter",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := len(loader.kindCalls), 1; got != want {
		t.Fatalf("ListFactsByKind() calls = %d, want %d", got, want)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "repository"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	call := loader.payloadCalls[0]
	if got, want := call.factKind, factKindContentEntity; got != want {
		t.Fatalf("payload fact kind = %q, want %q", got, want)
	}
	if got, want := call.payloadKey, "entity_type"; got != want {
		t.Fatalf("payload key = %q, want %q", got, want)
	}
	if got := strings.Join(call.payloadValues, ","); !strings.Contains(got, "Class") || !strings.Contains(got, "Function") {
		t.Fatalf("payload entity types = %q, want inheritable classes and override functions", got)
	}
	if got, want := len(writer.edgeRows()), 1; got != want {
		t.Fatalf("inheritance per-edge intents = %d, want %d", got, want)
	}
}

func TestSQLRelationshipHandlerUsesPayloadFilteredContentEntities(t *testing.T) {
	t.Parallel()

	loader := &recordingPayloadFactLoader{
		byPayload: []facts.Envelope{
			{
				FactKind: factKindContentEntity,
				Payload: map[string]any{
					"repo_id":     "repo-1",
					"entity_id":   "content-entity:table-1",
					"entity_type": "SqlTable",
					"entity_name": "public.users",
				},
			},
		},
		all: []facts.Envelope{{FactKind: "file"}},
		byKind: []facts.Envelope{{
			FactKind: factKindRepository,
			Payload:  map[string]any{"repo_id": "repo-1"},
		}},
	}
	writer := &recordingSQLRelationshipIntentWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-payload-filter",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := len(loader.kindCalls), 2; got != want {
		t.Fatalf("ListFactsByKind() calls = %d, want %d", got, want)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "repository"; got != want {
		t.Fatalf("ListFactsByKind() first kinds = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.kindCalls[1], ","), "file"; got != want {
		t.Fatalf("ListFactsByKind() second kinds = %q, want %q", got, want)
	}
	call := loader.payloadCalls[0]
	if got, want := call.factKind, factKindContentEntity; got != want {
		t.Fatalf("payload fact kind = %q, want %q", got, want)
	}
	if got, want := call.payloadKey, "entity_type"; got != want {
		t.Fatalf("payload key = %q, want %q", got, want)
	}
	if got, want := strings.Join(call.payloadValues, ","), "SqlTable,SqlColumn,SqlView,SqlFunction,SqlTrigger,SqlIndex"; got != want {
		t.Fatalf("payload entity types = %q, want %q", got, want)
	}
}

type recordingPayloadFactLoader struct {
	all            []facts.Envelope
	byKind         []facts.Envelope
	byPayload      []facts.Envelope
	listFactsCalls int
	kindCalls      [][]string
	payloadCalls   []recordingPayloadFilterCall
}

type recordingPayloadFilterCall struct {
	factKind      string
	payloadKey    string
	payloadValues []string
}

func (l *recordingPayloadFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	l.listFactsCalls++
	return append([]facts.Envelope(nil), l.all...), nil
}

func (l *recordingPayloadFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	factKinds []string,
) ([]facts.Envelope, error) {
	l.kindCalls = append(l.kindCalls, append([]string(nil), factKinds...))
	return append([]facts.Envelope(nil), l.byKind...), nil
}

func (l *recordingPayloadFactLoader) ListFactsByKindAndPayloadValue(
	_ context.Context,
	_ string,
	_ string,
	factKind string,
	payloadKey string,
	payloadValues []string,
) ([]facts.Envelope, error) {
	l.payloadCalls = append(l.payloadCalls, recordingPayloadFilterCall{
		factKind:      factKind,
		payloadKey:    payloadKey,
		payloadValues: append([]string(nil), payloadValues...),
	})
	return append([]facts.Envelope(nil), l.byPayload...), nil
}
