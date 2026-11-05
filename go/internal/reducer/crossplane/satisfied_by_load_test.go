// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplane

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// crossplaneLoadSpyWriter records the handler's retracts and edges.
type crossplaneLoadSpyWriter struct {
	retracts [][]string
	edges    []string
}

func (w *crossplaneLoadSpyWriter) WriteCrossplaneSatisfiedByEdges(_ context.Context, rows []map[string]any, _, _, _ string) error {
	for _, row := range rows {
		w.edges = append(w.edges, row["claim_uid"].(string)+"->"+row["xrd_uid"].(string))
	}
	return nil
}

func (w *crossplaneLoadSpyWriter) RetractCrossplaneSatisfiedByEdges(_ context.Context, scopeIDs []string, _, _ string) error {
	w.retracts = append(w.retracts, slices.Clone(scopeIDs))
	return nil
}

// crossplaneGenerationLoader is a loader without the one-snapshot read: its
// generation-keyed ListFacts returns the intent generation's own rows and its
// XRD read the cross-scope XRDs.
type crossplaneGenerationLoader struct {
	own, xrds []facts.Envelope
	listed    []string
}

func (l *crossplaneGenerationLoader) ListFacts(_ context.Context, scopeID, generationID string) ([]facts.Envelope, error) {
	l.listed = append(l.listed, scopeID+"/"+generationID)
	return l.own, nil
}

func (l *crossplaneGenerationLoader) ListActiveCrossplaneXRDFacts(context.Context) ([]facts.Envelope, error) {
	return l.xrds, nil
}

// crossplaneSnapshotLoader adds the one-snapshot read the store implements.
type crossplaneSnapshotLoader struct {
	crossplaneGenerationLoader
	snapshot []facts.Envelope
	err      error
	scopes   []string
}

func (l *crossplaneSnapshotLoader) ListActiveCrossplaneSatisfiedByFacts(_ context.Context, scopeID string) ([]facts.Envelope, error) {
	l.scopes = append(l.scopes, scopeID)
	return l.snapshot, l.err
}

func crossplaneLoadClaim(id string) facts.Envelope {
	return crossplaneContentEntityEnvelope(id, crossplaneEntityTypeK8sResource, map[string]any{
		"api_version": "delta.acme.internal/v1alpha1", "kind": "DeltaQueue",
	})
}

func crossplaneLoadXRD(id string) facts.Envelope {
	return crossplaneContentEntityEnvelope(id, crossplaneEntityTypeXRD, map[string]any{
		"group": "delta.acme.internal", "claim_kind": "DeltaQueue",
	})
}

func crossplaneLoadIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID: "intent-1", ScopeID: "app", GenerationID: "app-delta",
		Domain: reducercontract.DomainCrossplaneSatisfiedByMaterialization, AttemptCount: 1,
	}
}

// TestCrossplaneSatisfiedByHandlerPrefersTheSnapshotRead pins #10005 at the
// handler: a loader that offers ListActiveCrossplaneSatisfiedByFacts is read
// through it, keyed by the intent scope, and never through the
// generation-keyed own-scope read whose delta-only rows dropped unchanged
// claims. The retract still covers the intent scope, and the write carries
// every claim the snapshot read returned.
func TestCrossplaneSatisfiedByHandlerPrefersTheSnapshotRead(t *testing.T) {
	t.Parallel()

	loader := &crossplaneSnapshotLoader{
		crossplaneGenerationLoader: crossplaneGenerationLoader{own: []facts.Envelope{crossplaneLoadClaim("claim-2")}},
		snapshot:                   []facts.Envelope{crossplaneLoadClaim("claim-1"), crossplaneLoadClaim("claim-2"), crossplaneLoadXRD("xrd-1")},
	}
	writer := &crossplaneLoadSpyWriter{}
	handler := CrossplaneSatisfiedByMaterializationHandler{FactLoader: loader, EdgeWriter: writer}
	if _, err := handler.Handle(context.Background(), crossplaneLoadIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !slices.Equal(loader.scopes, []string{"app"}) || len(loader.listed) != 0 {
		t.Fatalf("snapshot reads = %v, generation reads = %v; want one snapshot read of app and no generation read", loader.scopes, loader.listed)
	}
	if len(writer.retracts) != 1 || !slices.Equal(writer.retracts[0], []string{"app"}) {
		t.Fatalf("retracts = %v, want [[app]]", writer.retracts)
	}
	slices.Sort(writer.edges)
	if want := []string{"claim-1->xrd-1", "claim-2->xrd-1"}; !slices.Equal(writer.edges, want) {
		t.Fatalf("edges = %v, want %v", writer.edges, want)
	}
}

// TestCrossplaneSatisfiedByHandlerFailsClosedOnSnapshotReadError pins that a
// failed snapshot read fails the intent before any retract: retracting and
// writing nothing would delete the scope's edges.
func TestCrossplaneSatisfiedByHandlerFailsClosedOnSnapshotReadError(t *testing.T) {
	t.Parallel()

	loader := &crossplaneSnapshotLoader{err: errors.New("snapshot read failed")}
	writer := &crossplaneLoadSpyWriter{}
	handler := CrossplaneSatisfiedByMaterializationHandler{FactLoader: loader, EdgeWriter: writer}
	if _, err := handler.Handle(context.Background(), crossplaneLoadIntent()); err == nil {
		t.Fatal("Handle() error = nil, want the snapshot read error")
	}
	if len(writer.retracts) != 0 || len(writer.edges) != 0 {
		t.Fatalf("retracts = %v, edges = %v; want none after a failed read", writer.retracts, writer.edges)
	}
}

// TestCrossplaneSatisfiedByHandlerKeepsTheGenerationReadFallback pins the
// fallback for a loader without the snapshot read: the intent generation's
// own rows plus the cross-scope XRDs, as before #10005.
func TestCrossplaneSatisfiedByHandlerKeepsTheGenerationReadFallback(t *testing.T) {
	t.Parallel()

	loader := &crossplaneGenerationLoader{
		own:  []facts.Envelope{crossplaneLoadClaim("claim-2")},
		xrds: []facts.Envelope{crossplaneLoadXRD("xrd-1")},
	}
	writer := &crossplaneLoadSpyWriter{}
	handler := CrossplaneSatisfiedByMaterializationHandler{FactLoader: loader, EdgeWriter: writer}
	if _, err := handler.Handle(context.Background(), crossplaneLoadIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !slices.Equal(loader.listed, []string{"app/app-delta"}) {
		t.Fatalf("generation reads = %v, want [app/app-delta]", loader.listed)
	}
	if want := []string{"claim-2->xrd-1"}; !slices.Equal(writer.edges, want) {
		t.Fatalf("edges = %v, want %v", writer.edges, want)
	}
}
