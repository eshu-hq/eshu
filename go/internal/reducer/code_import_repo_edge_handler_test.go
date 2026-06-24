// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// stubCodeImportFactLoader serves the scope-local file facts and the cross-scope
// package-ownership facts the code-import handler needs.
type stubCodeImportFactLoader struct {
	fileFacts      []facts.Envelope
	ownershipFacts []facts.Envelope
}

func (s *stubCodeImportFactLoader) ListFacts(
	_ context.Context,
	_ string,
	_ string,
) ([]facts.Envelope, error) {
	return s.fileFacts, nil
}

func (s *stubCodeImportFactLoader) ListActivePackageOwnershipFacts(
	_ context.Context,
) ([]facts.Envelope, error) {
	return s.ownershipFacts, nil
}

// codeImportOwnershipCorrelationFact builds a persisted ownership correlation
// fact carrying the package_id, owner repository_id, and outcome the decoder
// reads.
func codeImportOwnershipCorrelationFact(packageID, repositoryID, outcome string, observedAt time.Time) facts.Envelope {
	return facts.Envelope{
		FactID:        "ownership:" + packageID,
		FactKind:      packageOwnershipCorrelationFactKind,
		ObservedAt:    observedAt,
		StableFactKey: "ownership:" + packageID,
		Payload: map[string]any{
			"package_id":    packageID,
			"repository_id": repositoryID,
			"outcome":       outcome,
		},
	}
}

// TestCodeImportRepoEdgeHandlerProjectsEdgeForExternalImport proves the handler
// resolves a per-file external import to its owning repository through the
// cross-scope ownership facts and enqueues one DEPENDS_ON upsert intent on the
// shared repo-dependency lane with evidence_source = projection/code-imports.
func TestCodeImportRepoEdgeHandlerProjectsEdgeForExternalImport(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	packageID := "npm://registry.npmjs.org/express"
	loader := &stubCodeImportFactLoader{
		fileFacts: []facts.Envelope{
			makeCodeImportFileEnvelope("consumer-repo", "src/app.js", "javascript", []string{"express", "./local"}),
		},
		ownershipFacts: []facts.Envelope{
			packageRegistryPackageFact(packageID, "npm", "express", "", observedAt),
			codeImportOwnershipCorrelationFact(packageID, "owner-repo", "exact", observedAt),
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CodeImportRepoEdgeHandler{
		FactLoader:                 loader,
		OwnershipLoader:            loader,
		RepoDependencyIntentWriter: intentWriter,
		Now:                        func() time.Time { return observedAt },
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-code-import",
		ScopeID:      "git:consumer-repo",
		GenerationID: "gen-1",
		Domain:       DomainCodeImportRepoEdge,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if len(intentWriter.rows) != 1 {
		t.Fatalf("RepoDependencyIntentWriter calls = %d, want 1", len(intentWriter.rows))
	}
	rows := intentWriter.rows[0]
	if len(rows) != 1 {
		t.Fatalf("enqueued intents = %d, want 1", len(rows))
	}
	if got := rows[0].Payload["evidence_source"]; got != codeImportEvidenceSource {
		t.Errorf("evidence_source = %v, want %q", got, codeImportEvidenceSource)
	}
	if got := rows[0].Payload["repo_id"]; got != "consumer-repo" {
		t.Errorf("repo_id = %v, want consumer-repo", got)
	}
	if got := rows[0].Payload["target_repo_id"]; got != "owner-repo" {
		t.Errorf("target_repo_id = %v, want owner-repo", got)
	}
}

// TestCodeImportRepoEdgeHandlerRejectsWrongDomain proves the handler refuses an
// intent for any other domain.
func TestCodeImportRepoEdgeHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := CodeImportRepoEdgeHandler{
		FactLoader:                 &stubCodeImportFactLoader{},
		OwnershipLoader:            &stubCodeImportFactLoader{},
		RepoDependencyIntentWriter: &recordingRepoDependencyIntentWriter{},
	}
	if _, err := handler.Handle(context.Background(), Intent{Domain: DomainPackageSourceCorrelation}); err == nil {
		t.Fatal("Handle() error = nil, want non-nil for wrong domain")
	}
}

// TestCodeImportRepoEdgeHandlerNoOpWithoutWiring proves the handler is a no-op
// (no intents) when the intent writer or ownership loader is absent, keeping
// fact-only profiles unaffected.
func TestCodeImportRepoEdgeHandlerNoOpWithoutWiring(t *testing.T) {
	t.Parallel()

	loader := &stubCodeImportFactLoader{
		fileFacts: []facts.Envelope{
			makeCodeImportFileEnvelope("consumer-repo", "src/app.js", "javascript", []string{"express"}),
		},
	}
	handler := CodeImportRepoEdgeHandler{
		FactLoader:      loader,
		OwnershipLoader: loader,
		// RepoDependencyIntentWriter intentionally nil.
	}
	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-noop",
		ScopeID:      "git:consumer-repo",
		GenerationID: "gen-1",
		Domain:       DomainCodeImportRepoEdge,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
}

// TestCodeImportRepoEdgeHandlerSkipsUnresolvedImport proves that an import
// with no matching owner produces no upsert edge, but does enqueue a
// retract-only refresh intent so any stale projection/code-imports DEPENDS_ON
// edge from a prior generation is cleaned up (P1 retract behaviour).
func TestCodeImportRepoEdgeHandlerSkipsUnresolvedImport(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	loader := &stubCodeImportFactLoader{
		fileFacts: []facts.Envelope{
			makeCodeImportFileEnvelope("consumer-repo", "src/app.js", "javascript", []string{"unowned-pkg"}),
		},
		ownershipFacts: []facts.Envelope{
			packageRegistryPackageFact("npm://registry.npmjs.org/express", "npm", "express", "", observedAt),
			codeImportOwnershipCorrelationFact("npm://registry.npmjs.org/express", "owner-repo", "exact", observedAt),
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CodeImportRepoEdgeHandler{
		FactLoader:                 loader,
		OwnershipLoader:            loader,
		RepoDependencyIntentWriter: intentWriter,
		Now:                        func() time.Time { return observedAt },
	}
	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-unresolved",
		ScopeID:      "git:consumer-repo",
		GenerationID: "gen-1",
		Domain:       DomainCodeImportRepoEdge,
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	// One refresh/retract call is expected: the consumer is present with imports
	// but resolves no owner, so the handler emits a retract-only intent to drop
	// any stale code-import edge from a prior generation.
	if len(intentWriter.rows) != 1 {
		t.Fatalf("RepoDependencyIntentWriter calls = %d, want 1 (refresh/retract for unresolved import)", len(intentWriter.rows))
	}
	rows := intentWriter.rows[0]
	if len(rows) != 1 {
		t.Fatalf("enqueued intents = %d, want 1 refresh/retract intent", len(rows))
	}
	if got := anyToString(rows[0].Payload["action"]); got != "retract" {
		t.Errorf("action = %q, want retract", got)
	}
}

// TestCodeImportRepoEdgeHandlerEmitsRefreshIntentWhenOwnerlessFullSnapshot
// proves the handler calls BuildCodeImportRepoEdgeRefreshIntents and enqueues
// the resulting retract-only intent when a full snapshot produces zero upserts
// but at least one consumer repo appears in the file facts (issue #3651, P1).
// Without this fix the stale projection/code-imports DEPENDS_ON edge from a
// prior generation stays graph-visible forever.
func TestCodeImportRepoEdgeHandlerEmitsRefreshIntentWhenOwnerlessFullSnapshot(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	// File facts present a consumer with an import, but the ownership facts
	// resolve to no owner for any package in this scope.
	loader := &stubCodeImportFactLoader{
		fileFacts: []facts.Envelope{
			makeCodeImportFileEnvelope("consumer-repo", "src/app.js", "javascript", []string{"vanished-pkg"}),
		},
		ownershipFacts: []facts.Envelope{
			// ownership facts exist for a different package — none resolve vanished-pkg
			packageRegistryPackageFact("npm://registry.npmjs.org/other-pkg", "npm", "other-pkg", "", observedAt),
			codeImportOwnershipCorrelationFact("npm://registry.npmjs.org/other-pkg", "other-repo", "exact", observedAt),
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CodeImportRepoEdgeHandler{
		FactLoader:                 loader,
		OwnershipLoader:            loader,
		RepoDependencyIntentWriter: intentWriter,
		Now:                        func() time.Time { return observedAt },
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-refresh",
		ScopeID:      "git:consumer-repo",
		GenerationID: "gen-refresh",
		Domain:       DomainCodeImportRepoEdge,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if len(intentWriter.rows) != 1 {
		t.Fatalf("RepoDependencyIntentWriter calls = %d, want 1 (refresh intent)", len(intentWriter.rows))
	}
	rows := intentWriter.rows[0]
	if len(rows) != 1 {
		t.Fatalf("enqueued intents = %d, want 1 refresh/retract intent", len(rows))
	}
	row := rows[0]
	if got := anyToString(row.Payload["action"]); got != "retract" {
		t.Errorf("action = %q, want retract", got)
	}
	if got := anyToString(row.Payload["evidence_source"]); got != codeImportEvidenceSource {
		t.Errorf("evidence_source = %q, want %q", got, codeImportEvidenceSource)
	}
	if got := anyToString(row.Payload["repo_id"]); got != "consumer-repo" {
		t.Errorf("repo_id = %q, want consumer-repo", got)
	}
}
