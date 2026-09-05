// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This file is part of the #6060 lane-A P0 shim split out of
// family_code_shim.go to keep every file under the repo's 500-line
// cap. The deletion protocol in family_code_shim.go's header applies
// to every entry here: delete each entry with its named handler move.

import (
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// complexityAmbiguousError aliases the leaf-owned ambiguous-name refusal
// so the staying code handler and complexity reader keep constructing it
// unchanged. Delete with code.go's handler move.
type complexityAmbiguousError = codemodel.ComplexityAmbiguousError

// complexityNameCandidateLimit aliases the leaf-owned candidate cap so the
// staying complexity reader keeps its arithmetic unchanged. Delete with
// code_complexity_queries.go's reader move.
const complexityNameCandidateLimit = codemodel.ComplexityNameCandidateLimit

// readOnlyCypherCapability aliases the leaf-owned route capability so the
// staying Cypher route registration keeps naming it. Delete with
// code_cypher.go's handler move.
const readOnlyCypherCapability = codemodel.ReadOnlyCypherCapability

// visualizationGraphQueryCapability aliases the leaf-owned capability so
// the staying Cypher route registration keeps naming it. Delete with
// code_cypher.go's handler move.
const visualizationGraphQueryCapability = codemodel.VisualizationGraphQueryCapability

// writeComplexityAmbiguousError forwards to the leaf-owned refusal writer
// so the staying code handler keeps its call site unchanged. Delete with
// code.go's handler move.
func writeComplexityAmbiguousError(
	w http.ResponseWriter,
	r *http.Request,
	err complexityAmbiguousError,
	profile QueryProfile,
) {
	codemodel.WriteComplexityAmbiguousError(w, r, err, profile)
}

// complexityCandidateMaps forwards to the leaf-owned candidate shaper so
// the staying complexity reader keeps its call site unchanged. Delete with
// code_complexity_queries.go's reader move.
func complexityCandidateMaps(rows []map[string]any) []map[string]any {
	return codemodel.ComplexityCandidateMaps(rows)
}

// normalizeComplexityListLimit forwards to the leaf-owned list clamp so
// the staying code handler and complexity reader keep their call sites
// unchanged. Delete with code_complexity_queries.go's reader move.
func normalizeComplexityListLimit(limit int) int {
	return codemodel.NormalizeComplexityListLimit(limit)
}

// trimComplexityResults forwards to the leaf-owned list trimmer so the
// staying complexity reader keeps its call site unchanged. Delete with
// code_complexity_queries.go's reader move.
func trimComplexityResults(results []map[string]any, limit int) ([]map[string]any, bool) {
	return codemodel.TrimComplexityResults(results, limit)
}

// codeSearchProbeLimit forwards to the leaf-owned page probe so the
// staying code handler keeps its call site unchanged. Delete with code.go's
// handler move.
func codeSearchProbeLimit(publicLimit int) int {
	return codemodel.CodeSearchProbeLimit(publicLimit)
}

// codeSearchPagePayload forwards to the leaf-owned page shaper so the
// staying code handler keeps its call sites unchanged. Delete with code.go's
// handler move.
func codeSearchPagePayload(
	source string,
	sourceBackend string,
	query string,
	repositoryID string,
	rows []map[string]any,
	publicLimit int,
) map[string]any {
	return codemodel.CodeSearchPagePayload(source, sourceBackend, query, repositoryID, rows, publicLimit)
}

// buildSearchGraphEntitiesQuery forwards to the leaf-owned entity-search
// builder so the staying code handler and queryplan tests keep their call
// sites unchanged. Delete with code.go's handler move.
func buildSearchGraphEntitiesQuery(
	repoID string,
	query string,
	language string,
	limit int,
	exact bool,
	access repositoryAccessFilter,
) (string, map[string]any) {
	return codemodel.BuildSearchGraphEntitiesQuery(repoID, query, language, limit, exact, access)
}

// validateReadOnlyCypher forwards to the leaf-owned read-only guard so the
// staying Cypher route keeps its call site unchanged. Delete with
// code_cypher.go's handler move.
func validateReadOnlyCypher(cypher string) error {
	return codemodel.ValidateReadOnlyCypher(cypher)
}

// CodeHybridRanker aliases the leaf-owned find_code re-ranker so the
// staying code handler tests keep threading it opaquely. Delete with
// code.go's handler move.
type CodeHybridRanker = codemodel.CodeHybridRanker

// CodeResultReranker aliases the leaf-owned re-rank port so the staying
// code handler field and cmd wiring keep their types unchanged. Delete with
// code.go's handler move.
type CodeResultReranker = codemodel.CodeResultReranker

// NewCodeHybridRanker forwards to the leaf-owned re-ranker constructor so
// the staying tests and cmd wiring keep constructing it unchanged. Delete
// with code.go's handler move.
func NewCodeHybridRanker(enabled bool) *CodeHybridRanker {
	return codemodel.NewCodeHybridRanker(enabled)
}

// codeFlowFactKinds forwards to the leaf-owned kind table so the staying
// flow tests keep their call sites unchanged. Delete with code_flow.go's
// handler move.
func codeFlowFactKinds(kind CodeFlowKind) []string {
	return codemodel.CodeFlowFactKinds(kind)
}

// entityIDFromDocument forwards to the leaf-owned document-ID reader so
// the staying content-hybrid lane (ambiguous ownership, never edited)
// keeps passing it as a rank function unchanged. Delete with code.go's
// handler move.
func entityIDFromDocument(doc searchdocs.Document) string {
	return codemodel.EntityIDFromDocument(doc)
}

// listActiveCodeFlowFactsSQL aliases the leaf-owned flow read so the
// staying flow SQL tests keep asserting on its text. Delete with
// code_flow.go's handler move.
const listActiveCodeFlowFactsSQL = codemodel.ListActiveCodeFlowFactsSQL

// codeFlowKindFactKinds aliases the leaf-owned kind map so the staying
// flow tests keep ranging over it (same map). Delete with code_flow.go's
// handler move.
var codeFlowKindFactKinds = codemodel.CodeFlowKindFactKinds

// CodeFlowQueryer aliases the leaf-owned database port so the staying
// constructor forwarder and the cmd wiring keep passing a live *sql.DB
// through it. Delete with code_flow.go's handler move.
type CodeFlowQueryer = codemodel.CodeFlowQueryer

// PostgresCodeFlowStore aliases the leaf-owned flow reader so the staying
// constructor forwarder keeps its return type. Delete with code_flow.go's
// handler move.
type PostgresCodeFlowStore = codemodel.PostgresCodeFlowStore

// NewPostgresCodeFlowStore forwards to the leaf-owned flow reader
// constructor so the cmd wiring keeps constructing it unchanged. Delete
// with code_flow.go's handler move.
func NewPostgresCodeFlowStore(db CodeFlowQueryer) PostgresCodeFlowStore {
	return codemodel.NewPostgresCodeFlowStore(db)
}

// codeFlowFunctionFromPayload forwards to the leaf-owned record decoder so
// the staying flow tests keep their call sites unchanged. Delete with
// code_flow.go's handler move.
func codeFlowFunctionFromPayload(
	payload map[string]any,
	factID string,
	generationID string,
	factKind string,
	observedAt time.Time,
) CodeFlowFunction {
	return codemodel.CodeFlowFunctionFromPayload(payload, factID, generationID, factKind, observedAt)
}
