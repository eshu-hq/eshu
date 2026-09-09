// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestCodeSeamCrossPackageAccess proves the #6060 code seam is usable from
// outside package query -- the position the staying files named in the
// A-worklist (infra_graph_summary_packet.go, contract_code_flow.go,
// contract_capability_matrix.go, content_reader_structural_inventory.go,
// language_queries.go, language_query_metadata.go, entity_metadata.go,
// content_reader_dead_code.go, content_reader_dead_code_cross_repo.go,
// content_reader_security_secrets.go, content_reader.go,
// content_reader_code_topic.go, answer_packet_routes.go, and
// family_impact_change_surface_code.go) will be in once the 39-file code
// family moves to its own subpackage. It exercises the same reads those
// files make today through the unexported originals. If any accessor stops
// delegating, this fails; if any seam name is removed, it fails to compile.
func TestCodeSeamCrossPackageAccess(t *testing.T) {
	t.Parallel()

	// Consts: infra_graph_summary_packet.go, contract_code_flow.go, and
	// contract_capability_matrix.go only ever name these, so resolving them
	// is the whole of the cross-package proof.
	if codequery.CallGraphMetricsEdgeScanLimit != 50000 {
		t.Fatalf("CallGraphMetricsEdgeScanLimit = %d, want 50000", codequery.CallGraphMetricsEdgeScanLimit)
	}
	capabilities := []string{
		codequery.CodeFlowCFGSummaryCapability,
		codequery.CodeFlowPDGSummaryCapability,
		codequery.CodeFlowReachingDefCapability,
		codequery.CodeFlowTaintPathCapability,
		codequery.RouteToCallerCapability,
	}
	for _, capability := range capabilities {
		if capability == "" {
			t.Fatalf("capability seam const resolved empty: %v", capabilities)
		}
	}
	if codequery.StructuralInventoryDefaultLimit != 25 {
		t.Fatalf("StructuralInventoryDefaultLimit = %d, want 25", codequery.StructuralInventoryDefaultLimit)
	}

	// Var: family_impact_change_surface_code.go classifies errors from
	// fetchChangeSurfaceTopicRows against this sentinel.
	if !errors.Is(codequery.ErrCodeTopicBackendUnavailable, codequery.ErrCodeTopicBackendUnavailable) {
		t.Fatal("ErrCodeTopicBackendUnavailable does not compare equal to itself")
	}

	// CodeTopicEvidenceRow: family_impact_change_surface_code.go builds these
	// from ChangeSurfaceInvestigationRequest and reads them back through
	// AppendMatchedFile, CodeTopicSymbol, and CodeTopicEvidenceGroup.
	row := codequery.CodeTopicEvidenceRow{
		SourceKind:   "content_entity",
		RepoID:       "r1",
		RelativePath: "a/b.go",
		EntityID:     "e1",
		EntityName:   "Foo",
		EntityType:   "Function",
		Language:     "go",
		StartLine:    1,
		EndLine:      10,
		MatchedTerms: []string{"auth"},
		Score:        3,
	}
	files := codequery.AppendMatchedFile(nil, row)
	if len(files) != 1 || files[0]["relative_path"] != "a/b.go" {
		t.Fatalf("AppendMatchedFile(nil, row) = %#v, want one row for a/b.go", files)
	}
	group := codequery.CodeTopicEvidenceGroup(row, 1)
	if group["entity_id"] != "e1" {
		t.Fatalf("CodeTopicEvidenceGroup(row, 1) = %#v, want entity_id e1", group)
	}
	symbol := codequery.CodeTopicSymbol(row, 1)
	if symbol["entity_name"] != "Foo" {
		t.Fatalf("CodeTopicSymbol(row, 1) = %#v, want entity_name Foo", symbol)
	}
	terms := codequery.CodeTopicSearchTerms("locking synchronization", "", nil)
	if len(terms) == 0 {
		t.Fatal("CodeTopicSearchTerms(locking synchronization, ...) = empty, want at least one term")
	}

	// CodeTopicInvestigationRequest: answer_packet_routes.go and
	// content_reader_code_topic.go both build and read this request shape
	// from outside the code move set.
	investigation := codequery.CodeTopicInvestigationRequest{
		Topic:                "auth flow",
		RepoID:               "r1",
		Terms:                terms,
		Limit:                10,
		AllowedRepositoryIDs: []string{"r1", "r2"},
	}
	if investigation.Topic != "auth flow" || len(investigation.AllowedRepositoryIDs) != 2 {
		t.Fatalf("CodeTopicInvestigationRequest round trip = %+v, want Topic=auth flow and 2 allowed repositories", investigation)
	}

	// deadcode.CrossRepoDeadCodeConfidenceLabel and deadcode.CrossRepoDeadCodeEvidence:
	// content_reader_dead_code_cross_repo.go labels evidence rows with the
	// first and constructs them as the second.
	label := deadcode.CrossRepoDeadCodeConfidenceLabel(0.3)
	evidence := deadcode.CrossRepoDeadCodeEvidence{
		ConsumerRepoID:  "r2",
		Confidence:      0.3,
		ConfidenceLabel: label,
	}
	if evidence.ConfidenceLabel != "low" {
		t.Fatalf("deadcode.CrossRepoDeadCodeConfidenceLabel(0.3) = %q, want low", evidence.ConfidenceLabel)
	}

	// deadcode.CodeReachabilityCoverage: content_reader_dead_code.go's
	// deadcode.CodeReachabilityCoverage method returns this shape.
	coverage := deadcode.CodeReachabilityCoverage{Available: true, Truncated: false}
	if !coverage.Available {
		t.Fatal("deadcode.CodeReachabilityCoverage{Available: true} did not round trip")
	}

	// deadcode.MergeStrongestDeadCodeIncomingEdge: content_reader_dead_code.go's
	// mergeDeadCodeIncomingEdge forwards to this from outside the code move
	// set once the family moves.
	incoming := map[string]querycontract.DeadCodeIncomingEdge{}
	deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, "e1", querycontract.DeadCodeIncomingEdge{MaxConfidence: 0.5, Method: "graph_call"})
	deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, "e1", querycontract.DeadCodeIncomingEdge{HiddenConsumer: true})
	if got := incoming["e1"]; got.MaxConfidence != 0.5 || !got.HiddenConsumer {
		t.Fatalf("deadcode.MergeStrongestDeadCodeIncomingEdge result = %+v, want confidence 0.5 retained plus HiddenConsumer merged in", got)
	}

	// ResultContentEntityType: entity_metadata.go resolves a graph result's
	// content entity type through this before an entity-id metadata lookup.
	if got := codequery.ResultContentEntityType(map[string]any{"labels": []string{"Class"}}); got != "Class" {
		t.Fatalf("ResultContentEntityType(labels=[Class]) = %q, want Class", got)
	}

	// LanguageQueryGrant / LanguageQueryGrantFor: language_queries.go reads
	// Access, language_query_metadata.go reads AllowedRepositoryIDs.
	grant, blocked := codequery.LanguageQueryGrantFor(context.Background(), "")
	if blocked {
		t.Fatal("LanguageQueryGrantFor(unscoped) reported blocked, want false")
	}
	if !grant.Access.AllScopes {
		t.Fatalf("LanguageQueryGrant.Access = %+v, want AllScopes", grant.Access)
	}
	if grant.AllowedRepositoryIDs != nil {
		t.Fatalf("LanguageQueryGrant.AllowedRepositoryIDs = %v, want nil", grant.AllowedRepositoryIDs)
	}

	// ApplyRepositorySelectorForAccess: POST /api/v0/code/language-query's
	// handler-independent selector resolution calls this from outside the
	// code move set.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", nil)
	if !codequery.ApplyRepositorySelectorForAccess(w, r, nil, nil, nil, codequery.RouteToCallerCapability) {
		t.Fatal("ApplyRepositorySelectorForAccess(nil selector) = false, want true")
	}

	// StructuralInventoryRequest: content_reader_structural_inventory.go
	// builds the WHERE clause from this shape and calls its exported
	// EntityType/Kind/NormalizedLimit methods -- renamed at their
	// declaration rather than aliased, since Go has no method aliases.
	inv := codequery.StructuralInventoryRequest{InventoryKind: "documented_function", EntityKind: "function", RepoID: "r1"}
	if inv.Kind() != "documented_function" {
		t.Fatalf("StructuralInventoryRequest.Kind() = %q, want documented_function", inv.Kind())
	}
	if inv.EntityType() != "Function" {
		t.Fatalf("StructuralInventoryRequest.EntityType() = %q, want Function", inv.EntityType())
	}
	if inv.NormalizedLimit() != codequery.StructuralInventoryDefaultLimit {
		t.Fatalf("StructuralInventoryRequest.NormalizedLimit() = %d, want default", inv.NormalizedLimit())
	}

	// SymbolSearchRequest: content_reader_symbol_search.go builds its filters
	// and args from this shape's exported ResolvedSymbol/MustMatchMode/
	// NormalizedEntityTypes methods.
	sym := codequery.SymbolSearchRequest{Symbol: " Foo ", MatchMode: "fuzzy", EntityType: "function"}
	if sym.ResolvedSymbol() != "Foo" {
		t.Fatalf("SymbolSearchRequest.ResolvedSymbol() = %q, want Foo", sym.ResolvedSymbol())
	}
	if sym.MustMatchMode() != "fuzzy" {
		t.Fatalf("SymbolSearchRequest.MustMatchMode() = %q, want fuzzy", sym.MustMatchMode())
	}
	if got, want := sym.NormalizedEntityTypes(), []string{"Function"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SymbolSearchRequest.NormalizedEntityTypes() = %v, want %v", got, want)
	}

	// HardcodedSecretFindingRow / HardcodedSecretInvestigationRequest:
	// content_reader_security_secrets.go builds and returns these shapes
	// from outside the code move set.
	secretReq := codequery.HardcodedSecretInvestigationRequest{RepoID: "r1", FindingKinds: []string{"api_token"}}
	secretRow := codequery.HardcodedSecretFindingRow{RepoID: secretReq.RepoID, FindingKind: "api_token", Confidence: "high"}
	if secretRow.RepoID != "r1" || secretRow.Confidence != "high" {
		t.Fatalf("HardcodedSecretFindingRow round trip = %+v, want RepoID r1 Confidence high", secretRow)
	}

	// Interface seams: content_reader.go's compile-time tripwire block and
	// content_reader_code_topic.go/content_reader_symbol_search.go's own
	// signatures name these from outside the code move set.
	var _ codequery.CodeTopicContentInvestigator
	var _ codequery.HardcodedSecretInvestigator
	var _ codequery.SymbolContentSearcher
	// Root's ContentReader still satisfies the secret investigator seam; this
	// external test package may import root package query without a cycle.
	var _ codequery.HardcodedSecretInvestigator = (*query.ContentReader)(nil)
}
