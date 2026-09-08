// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestCodeSeamExportsForward is the tripwire for the #6060 code seam export:
// every seam forwarder returns what its backing home returns on the same
// input, every seam alias names its home type, and every seam const pins its
// contract value. If a forwarder body diverges (or a seam name is removed),
// this fails. It also fails to COMPILE if any seam name is removed, which is
// the point -- the code move depends on each of these names resolving from
// outside the code subpackage.
func TestCodeSeamExportsForward(t *testing.T) {
	t.Parallel()

	if CallGraphMetricsEdgeScanLimit != 50000 {
		t.Fatalf("CallGraphMetricsEdgeScanLimit = %d, want 50000", CallGraphMetricsEdgeScanLimit)
	}
	if CodeFlowCFGSummaryCapability != "code_flow.cfg_summary" {
		t.Fatalf("CodeFlowCFGSummaryCapability = %q, want code_flow.cfg_summary", CodeFlowCFGSummaryCapability)
	}
	if CodeFlowPDGSummaryCapability != "code_flow.pdg_summary" {
		t.Fatalf("CodeFlowPDGSummaryCapability = %q, want code_flow.pdg_summary", CodeFlowPDGSummaryCapability)
	}
	if CodeFlowReachingDefCapability != "code_flow.reaching_def" {
		t.Fatalf("CodeFlowReachingDefCapability = %q, want code_flow.reaching_def", CodeFlowReachingDefCapability)
	}
	if CodeFlowTaintPathCapability != "code_flow.taint_path" {
		t.Fatalf("CodeFlowTaintPathCapability = %q, want code_flow.taint_path", CodeFlowTaintPathCapability)
	}
	if RouteToCallerCapability != "call_graph.route_to_caller" {
		t.Fatalf("RouteToCallerCapability = %q, want call_graph.route_to_caller", RouteToCallerCapability)
	}
	if StructuralInventoryDefaultLimit != 25 {
		t.Fatalf("StructuralInventoryDefaultLimit = %d, want 25", StructuralInventoryDefaultLimit)
	}
	if CrossRepoDeadCodeUngrantedConsumerProbeQuery != crossRepoDeadCodeUngrantedConsumerProbeQuery {
		t.Fatal("CrossRepoDeadCodeUngrantedConsumerProbeQuery != crossRepoDeadCodeUngrantedConsumerProbeQuery")
	}
	if !strings.Contains(CrossRepoDeadCodeUngrantedConsumerProbeQuery, "code_reachability_rows") {
		t.Fatalf("CrossRepoDeadCodeUngrantedConsumerProbeQuery = %q, want it to reference code_reachability_rows", CrossRepoDeadCodeUngrantedConsumerProbeQuery)
	}

	if !errors.Is(ErrCodeTopicBackendUnavailable, errCodeTopicBackendUnavailable) {
		t.Fatal("ErrCodeTopicBackendUnavailable != errCodeTopicBackendUnavailable")
	}

	gotCypher, gotParams := CallGraphMetricsEdgesCypher(" r1 ")
	wantCypher, wantParams := callGraphMetricsEdgesCypher(" r1 ")
	if gotCypher != wantCypher || !reflect.DeepEqual(gotParams, wantParams) {
		t.Fatal("CallGraphMetricsEdgesCypher != callGraphMetricsEdgesCypher")
	}
	if wantParams["repo_id"] != "r1" {
		t.Fatalf("callGraphMetricsEdgesCypher params repo_id = %v, want trimmed r1", wantParams["repo_id"])
	}

	row := CodeTopicEvidenceRow{
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
	gotFiles := AppendMatchedFile(nil, row)
	wantFiles := appendMatchedFile(nil, row)
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatal("AppendMatchedFile != appendMatchedFile")
	}
	if len(gotFiles) != 1 || gotFiles[0]["repo_id"] != "r1" {
		t.Fatalf("AppendMatchedFile(nil, row) = %#v, want one row for r1", gotFiles)
	}
	// A second call for the same repo/path is deduplicated -- proves the seam
	// call reached the real dedup loop, not a stub that always appends.
	if gotFiles = AppendMatchedFile(gotFiles, row); len(gotFiles) != 1 {
		t.Fatalf("AppendMatchedFile did not dedupe an existing (repo_id, relative_path): %#v", gotFiles)
	}

	gotGroup := CodeTopicEvidenceGroup(row, 1)
	wantGroup := codeTopicEvidenceGroup(row, 1)
	if !reflect.DeepEqual(gotGroup, wantGroup) {
		t.Fatal("CodeTopicEvidenceGroup != codeTopicEvidenceGroup")
	}
	if gotGroup["entity_id"] != "e1" || gotGroup["rank"] != 1 {
		t.Fatalf("CodeTopicEvidenceGroup(row, 1) = %#v, want entity_id e1 rank 1", gotGroup)
	}

	gotSymbol := CodeTopicSymbol(row, 2)
	wantSymbol := codeTopicSymbol(row, 2)
	if !reflect.DeepEqual(gotSymbol, wantSymbol) {
		t.Fatal("CodeTopicSymbol != codeTopicSymbol")
	}
	if gotSymbol["entity_name"] != "Foo" || gotSymbol["rank"] != 2 {
		t.Fatalf("CodeTopicSymbol(row, 2) = %#v, want entity_name Foo rank 2", gotSymbol)
	}

	gotTerms := CodeTopicSearchTerms("authentication flow", "", nil)
	wantTerms := codeTopicSearchTerms("authentication flow", "", nil)
	if !reflect.DeepEqual(gotTerms, wantTerms) {
		t.Fatal("CodeTopicSearchTerms != codeTopicSearchTerms")
	}
	if len(gotTerms) == 0 {
		t.Fatal("CodeTopicSearchTerms(authentication flow, ...) = empty, want at least one term")
	}

	if got, want := CrossRepoDeadCodeConfidenceLabel(0.95), crossRepoDeadCodeConfidenceLabel(0.95); got != want || got != "high" {
		t.Fatalf("CrossRepoDeadCodeConfidenceLabel(0.95) = %q, want high", got)
	}
	if got := CrossRepoDeadCodeConfidenceLabel(0); got != "unknown" {
		t.Fatalf("CrossRepoDeadCodeConfidenceLabel(0) = %q, want unknown", got)
	}

	result := map[string]any{"labels": []string{"Function"}}
	if got, want := ResultContentEntityType(result), resultContentEntityType(result); got != want || got != "Function" {
		t.Fatalf("ResultContentEntityType(labels=[Function]) = %q, want Function", got)
	}
	if got := ResultContentEntityType(map[string]any{"labels": []string{"NotAKnownLabel"}}); got != "" {
		t.Fatalf("ResultContentEntityType(unknown label) = %q, want empty", got)
	}

	// A stronger edge overwrites a weaker one's confidence but unions the
	// earlier HiddenConsumer marker rather than dropping it.
	incoming := map[string]querycontract.DeadCodeIncomingEdge{
		"e1": {HiddenConsumer: true},
	}
	MergeStrongestDeadCodeIncomingEdge(incoming, "e1", querycontract.DeadCodeIncomingEdge{MaxConfidence: 0.9, Method: "graph_call"})
	if got := incoming["e1"]; got.MaxConfidence != 0.9 || got.Method != "graph_call" || !got.HiddenConsumer {
		t.Fatalf("MergeStrongestDeadCodeIncomingEdge result = %+v, want confidence 0.9 method graph_call HiddenConsumer true", got)
	}
	// A weaker edge merged in afterward must not downgrade the stored
	// confidence, proving "strongest" rather than "latest" wins.
	MergeStrongestDeadCodeIncomingEdge(incoming, "e1", querycontract.DeadCodeIncomingEdge{MaxConfidence: 0.2, Method: "legacy"})
	if got := incoming["e1"]; got.MaxConfidence != 0.9 || got.Method != "graph_call" {
		t.Fatalf("MergeStrongestDeadCodeIncomingEdge downgraded the strongest edge: %+v", got)
	}

	// LanguageQueryGrantFor: an unscoped caller (no AuthContext at all) is not
	// blocked and carries an AllScopes grant with no bound repository list.
	grant, blocked := LanguageQueryGrantFor(context.Background(), "")
	if blocked {
		t.Fatal("LanguageQueryGrantFor(unscoped) reported blocked, want false")
	}
	if !grant.Access.AllScopes {
		t.Fatalf("LanguageQueryGrantFor(unscoped) grant.Access = %+v, want AllScopes", grant.Access)
	}
	if grant.AllowedRepositoryIDs != nil {
		t.Fatalf("LanguageQueryGrantFor(unscoped) grant.AllowedRepositoryIDs = %v, want nil", grant.AllowedRepositoryIDs)
	}
	// A scoped caller with no grants at all is blocked before any read.
	scopedEmptyCtx := queryauth.ContextWithAuthContext(context.Background(), queryauth.AuthContext{Mode: queryauth.AuthModeScoped})
	if _, blocked := LanguageQueryGrantFor(scopedEmptyCtx, ""); !blocked {
		t.Fatal("LanguageQueryGrantFor(scoped, no grants) reported not blocked, want true")
	}
	wantGrant, wantBlocked := languageQueryGrantFor(context.Background(), "")
	if blocked != wantBlocked || !reflect.DeepEqual(grant, LanguageQueryGrant(wantGrant)) {
		t.Fatal("LanguageQueryGrantFor != languageQueryGrantFor")
	}

	// ApplyRepositorySelectorForAccess is exported at its own declaration
	// (code_repository_selector.go), not forwarded from here -- a forwarder
	// would add a third capability-parameter call site that
	// TestWriteGraphReadErrorCapabilitiesExistInMatrix's caller-tracing sweep
	// cannot resolve, since nothing in production would call the forwarder
	// itself. It stays in this tripwire because it is still part of the
	// #6060 exported surface language_queries.go crosses to reach. A nil
	// selector short-circuits to true before touching graph or content,
	// which is a real branch of the function, not a stub -- a non-nil
	// graph/content would panic on Run if this exercised anything else.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", nil)
	if !ApplyRepositorySelectorForAccess(w, r, nil, nil, nil, RouteToCallerCapability) {
		t.Fatal("ApplyRepositorySelectorForAccess(nil selector) = false, want true")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("ApplyRepositorySelectorForAccess(nil selector) wrote a response body %q, want none", w.Body.String())
	}

	// Aliases name the same objects: assigning an original-typed value to an
	// alias-typed variable only compiles when the alias holds, so each line
	// below is a compile-time identity proof.
	assertSeamAlias[CodeReachabilityCoverage](codeReachabilityCoverage{Available: true})
	assertSeamAlias[CodeTopicEvidenceRow](codeTopicEvidenceRow{})
	assertSeamAlias[CodeTopicInvestigationRequest](codeTopicInvestigationRequest{})
	assertSeamAlias[CrossRepoDeadCodeEvidence](crossRepoDeadCodeEvidence{})
	assertSeamAlias[HardcodedSecretFindingRow](hardcodedSecretFindingRow{})
	assertSeamAlias[HardcodedSecretInvestigationRequest](hardcodedSecretInvestigationRequest{})
	assertSeamAlias[LanguageQueryGrant](languageQueryGrant{})
	assertSeamAlias[StructuralInventoryRequest](structuralInventoryRequest{})
	assertSeamAlias[SymbolSearchRequest](symbolSearchRequest{})
	var _ CodeTopicContentInvestigator
	var _ HardcodedSecretInvestigator
	var _ SymbolContentSearcher
	var _ hardcodedSecretInvestigator = (*ContentReader)(nil)

	// Renamed structuralInventoryRequest/symbolSearchRequest methods resolve
	// under their exported spelling and compute the same normalization the
	// unexported callers in this package still rely on.
	inv := StructuralInventoryRequest{InventoryKind: "documented_function", EntityKind: "function"}
	if inv.Kind() != "documented_function" {
		t.Fatalf("StructuralInventoryRequest.Kind() = %q, want documented_function", inv.Kind())
	}
	if inv.EntityType() != "Function" {
		t.Fatalf("StructuralInventoryRequest.EntityType() = %q, want Function", inv.EntityType())
	}
	if inv.NormalizedLimit() != StructuralInventoryDefaultLimit {
		t.Fatalf("StructuralInventoryRequest.NormalizedLimit() = %d, want default %d", inv.NormalizedLimit(), StructuralInventoryDefaultLimit)
	}
	// QueryLimit and NormalizedLimit diverge above the display cap: QueryLimit
	// (the SQL LIMIT content_reader_structural_inventory.go binds) does not
	// clamp, while NormalizedLimit (the display cap reported to the caller)
	// does. An over-cap Limit distinguishes them; a value at or below the cap
	// would not.
	overCap := StructuralInventoryRequest{Limit: 9000}
	if got, want := overCap.QueryLimit(), 9000; got != want {
		t.Fatalf("StructuralInventoryRequest.QueryLimit() = %d, want unclamped %d", got, want)
	}
	if got := overCap.NormalizedLimit(); got == 9000 {
		t.Fatalf("StructuralInventoryRequest.NormalizedLimit() = %d, want it to clamp below QueryLimit", got)
	}
	sym := SymbolSearchRequest{Symbol: " Foo ", MatchMode: "exact", EntityType: "function"}
	if sym.ResolvedSymbol() != "Foo" {
		t.Fatalf("SymbolSearchRequest.ResolvedSymbol() = %q, want Foo", sym.ResolvedSymbol())
	}
	if sym.MustMatchMode() != "exact" {
		t.Fatalf("SymbolSearchRequest.MustMatchMode() = %q, want exact", sym.MustMatchMode())
	}
	if got, want := sym.NormalizedEntityTypes(), []string{"Function"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SymbolSearchRequest.NormalizedEntityTypes() = %v, want %v", got, want)
	}
}
