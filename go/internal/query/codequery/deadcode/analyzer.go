// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"go.opentelemetry.io/otel/trace"
)

// ContentStore is the content backend the analysis reads entity content
// from. It aliases the leaf contract so Analyzer does not import codequery.
type ContentStore = querycontract.ContentStore

// GraphQuery is the read-only graph backend the analysis runs candidate
// and evidence Cypher against.
type GraphQuery = querycontract.GraphQuery

// QueryProfile names the runtime profile the analysis executes under.
type QueryProfile = querycontract.QueryProfile

// RepositoryAccessFilter carries the caller's repository grant.
type RepositoryAccessFilter = querycontract.RepositoryAccessFilter

// TruthEnvelope carries the evidence basis of a dead-code response packet.
type TruthEnvelope = querycontract.TruthEnvelope

// TruthBasis names the evidence basis of a truth envelope.
type TruthBasis = querycontract.TruthBasis

// ErrorCode names a stable API error code.
type ErrorCode = querycontract.ErrorCode

// DeadCodeIncomingEdge is one resolved incoming edge of a candidate.
type DeadCodeIncomingEdge = querycontract.DeadCodeIncomingEdge

// EntityContent is one entity's content payload.
type EntityContent = querycontract.EntityContent

// RepositoryContentCoverage carries per-repository content coverage.
type RepositoryContentCoverage = querycontract.RepositoryContentCoverage

// Dependencies carries everything the dead-code analysis needs from the
// staying codequery handler package. The value fields come straight off
// CodeHandler; the func fields point at staying codequery helpers and
// methods that cannot move here (queryplan-pinned row readers, the
// package-wide HTTP writers, grant filters) without breaking import
// direction or frozen source digests. CodeHandler's delegates build a
// Dependencies per call, so Analyzer stays stateless and safe for
// concurrent HTTP use. When a func-field helper moves to a leaf package,
// repoint the field to the leaf and drop the codequery reference.
type Dependencies struct {
	// Content is the entity-content backend (CodeHandler.Content).
	Content ContentStore
	// Graph is the read-only graph backend (CodeHandler.Neo4j).
	Graph GraphQuery
	// Profile is the already-resolved runtime profile (h.profile()).
	Profile QueryProfile

	// CandidateRows runs the pinned candidate-row reader that stays in
	// codequery (deadCodeCandidateRows).
	CandidateRows func(ctx context.Context, repoID, label, language string, limit, offset int) ([]map[string]any, error)
	// IncomingEdges runs the pinned incoming-edge reader that stays in
	// codequery (deadCodeResultsWithGraphIncomingEdges).
	IncomingEdges func(ctx context.Context, results []map[string]any, label string) (map[string]DeadCodeIncomingEdge, error)
	// ApplySelector enforces the staying repository-selector gate
	// (CodeHandler.applyRepositorySelectorForCapability).
	ApplySelector func(w http.ResponseWriter, r *http.Request, selector *string, capability string) bool
	// GrantFilter builds the caller's repository access filter
	// (codeGrantAccessFilter).
	GrantFilter func(ctx context.Context) RepositoryAccessFilter
	// GrantScope resolves the content-grant scope for a repository
	// (codeContentGrantScope).
	GrantScope func(ctx context.Context, repoID string) (allowed []string, blocked bool)

	// ResultEntityIDs extracts entity IDs from candidate result rows
	// (deadcode helper DeadCodeResultEntityIDs).
	ResultEntityIDs func(results []map[string]any) []string
	// NextCalls builds the recommended follow-up calls for an
	// investigation scan (staying codequery helper
	// deadCodeInvestigationNextCalls).
	NextCalls func(scan DeadCodeInvestigationScan) []map[string]any
	// StartSpan opens a handler tracing span (staying codequery helper
	// startQueryHandlerSpan); the caller ends it with span.End().
	StartSpan func(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span)
	// MergeMetadata merges content metadata over graph metadata.
	MergeMetadata func(existing any, content map[string]any) map[string]any
	// FilterResponse filters a relationship response by direction and
	// type (staying codequery helper filterRelationshipResponse).
	FilterResponse func(response map[string]any, direction, relationshipType string) map[string]any

	// WriteSuccess writes a successful JSON response packet.
	WriteSuccess func(w http.ResponseWriter, r *http.Request, status int, data any, truth *TruthEnvelope)
	// WriteError writes a stable error response.
	WriteError func(w http.ResponseWriter, status int, message string)
	// WriteContractError writes the capability/profile contract error.
	WriteContractError func(w http.ResponseWriter, r *http.Request, status int, message string, errCode ErrorCode, capability string, currentProfile, requiredProfile QueryProfile)
	// WriteGraphReadError writes the bounded graph-read error contract,
	// reporting whether it handled err.
	WriteGraphReadError func(w http.ResponseWriter, r *http.Request, err error, capability string) bool
	// ReadJSON decodes a request body.
	ReadJSON func(r *http.Request, v any) error
}

// Analyzer runs dead-code analysis with explicitly injected dependencies.
// It holds no request state: CodeHandler's delegates construct one per
// call from the handler's own fields, so concurrent requests never share
// an Analyzer.
type Analyzer struct {
	deps Dependencies
}

// NewAnalyzer returns an Analyzer bound to deps. A nil Content or Graph
// backend is tolerated the same way a nil CodeHandler is: methods that
// need the backend return empty results or errors instead of panicking.
func NewAnalyzer(deps Dependencies) *Analyzer {
	return &Analyzer{deps: deps}
}
