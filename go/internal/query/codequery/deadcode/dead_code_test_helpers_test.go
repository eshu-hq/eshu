// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/search"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func assertQueryTestStringSliceEqual(t *testing.T, got any, want []string) {
	t.Helper()

	gotSlice, ok := got.([]any)
	if !ok {
		t.Fatalf("string slice type = %T, want []any", got)
	}
	if len(gotSlice) != len(want) {
		t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
	}
	for i, wantValue := range want {
		if gotValue, ok := gotSlice[i].(string); !ok || gotValue != wantValue {
			t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
		}
	}
}

// buildDeadCodeGraphCypher renders the dead-code candidate scan for the default
// Function label with an unscoped access filter, so the older shape tests can
// assert the Cypher skeleton without building a grant. It lives in a test file
// on purpose: an all-scopes filter is not something production code may hand
// the builder, because the scan's authorization seam IS the access argument
// that deadcode.BuildDeadCodeGraphCypherForLabel takes. Production callers reach the
// builder through scanDeadCodeCandidates, which derives access from the
// request. The querycontract.GraphBackend argument is unused -- both backends render the same
// Cypher -- and stays only so the backend-matrix tests read naturally.
func buildDeadCodeGraphCypher(hasRepoID bool, _ querycontract.GraphBackend) string {
	return deadcode.BuildDeadCodeGraphCypherForLabel(hasRepoID, "Function", "", deadcode.RepositoryAccessFilter{AllScopes: true})
}

// TestMain registers the dead-code capability row the moved HTTP tests drive
// through. Production registers it from root package query's
// contract_capability_matrix.go init(), which this package's test binary does
// not import (root imports the leaves, so that would be an import cycle).
// Without the row every handler answers 501 unsupported_capability. The
// values mirror that matrix entry; separate ceiling variables follow
// querycontract's aliasing note.
func TestMain(m *testing.M) {
	authoritative := querycontract.TruthLevelDerived
	fullStack := querycontract.TruthLevelDerived
	production := querycontract.TruthLevelDerived
	querycontract.SetCapabilitySupport("code_quality.dead_code", querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &authoritative,
		LocalFullStackMax:     &fullStack,
		ProductionMax:         &production,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	})
	os.Exit(m.Run())
}

// Grant fixture ids for the moved dead-code tests. Values match the staying
// codequery grant tests that coined them (auth_scoped_code_topic_grant_test.go
// and auth_scoped_code_dead_code_cross_repo_grant_test.go); a symbol declared
// in a _test.go file cannot be imported across a package boundary, so the
// moved family carries its own copy.
const (
	codeGrantGrantedRepo  = "repo://tenant-a/granted-service"
	codeGrantConsumerRepo = "repo://tenant-a/consumer-service"
	codeGrantOtherRepo    = "repo://tenant-b/other-service"
)

// deadCodeHiddenConsumerEntityID matches the staying hidden-consumer test's
// sentinel (auth_scoped_code_dead_code_hidden_consumer_test.go).
const deadCodeHiddenConsumerEntityID = "repo://tenant-a/granted-service#unusedHelper"

// codeContentGrantAdmits mirrors the `repo_id = $n` / `repo_id = ANY($n)`
// pair the shipped SQL builders emit: an explicit repo_id anchors the scan, a
// non-empty grant list restricts it, and an empty grant list does not. Copy
// of codequery's grant_shared_test.go helper, which cannot be imported here.
func codeContentGrantAdmits(rowRepoID, repoID string, allowedRepositoryIDs []string) bool {
	if anchor := strings.TrimSpace(repoID); anchor != "" && rowRepoID != anchor {
		return false
	}
	if len(allowedRepositoryIDs) == 0 {
		return true
	}
	for _, id := range allowedRepositoryIDs {
		if id == rowRepoID {
			return true
		}
	}
	return false
}

// deadCodeGrantContentStore proves the producer-side candidate scan binds the
// grant. Copy of codequery's auth_scoped_code_dead_code_grant_test.go double
// (over the moved fakeDeadCodeContentStore), whose filter mirrors the shipped
// SQL: an explicit repo_id anchors the scan, a non-empty grant list restricts
// it, an empty list does not restrict it at all.
type deadCodeGrantContentStore struct {
	fakeDeadCodeContentStore
	bound   []string
	queried bool
}

func (s *deadCodeGrantContentStore) DeadCodeCandidateRows(
	_ context.Context,
	q codeshaping.DeadCodeCandidateQuery,
) ([]map[string]any, error) {
	s.queried = true
	s.bound = append([]string(nil), q.AllowedRepositoryIDs...)
	if q.Label != "Function" || q.Offset > 0 {
		return nil, nil
	}
	rows := make([]map[string]any, 0, 2)
	for _, repoID := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		if !codeContentGrantAdmits(repoID, q.RepoID, q.AllowedRepositoryIDs) {
			continue
		}
		rows = append(rows, map[string]any{
			"entity_id":  repoID + "#unusedHelper",
			"name":       "unusedHelper",
			"labels":     []any{"Function"},
			"file_path":  "internal/legacy/helper.go",
			"repo_id":    repoID,
			"repo_name":  repoID,
			"language":   "go",
			"start_line": 4,
			"end_line":   9,
		})
	}
	return rows, nil
}

// crossRepoDeadCodeGrantStore proves the cross-repo consumer reads bind the
// grant. Copy of codequery's
// auth_scoped_code_dead_code_cross_repo_grant_test.go double.
type crossRepoDeadCodeGrantStore struct {
	deadCodeGrantContentStore
	boundConsumerGrant []string
	signalRead         bool
}

func (s *crossRepoDeadCodeGrantStore) CrossRepoDeadCodeConsumerEvidence(
	_ context.Context,
	producerRepoID string,
	entityIDs []string,
	reads querycontract.CrossRepoDeadCodeConsumerReads,
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, querycontract.CrossRepoDeadCodeHiddenConsumers, error) {
	s.boundConsumerGrant = append([]string(nil), reads.PageRepositoryIDs...)
	s.signalRead = len(reads.SignalGrant) > 0
	evidence := make(map[string][]deadcode.CrossRepoDeadCodeEvidence, len(entityIDs))
	hidden := querycontract.CrossRepoDeadCodeHiddenConsumers{}
	for _, entityID := range entityIDs {
		for _, consumerRepoID := range []string{codeGrantConsumerRepo, codeGrantOtherRepo} {
			if consumerRepoID == producerRepoID {
				continue
			}
			row := crossRepoDeadCodeGrantConsumerRow(consumerRepoID, entityID)
			// The probe answers over the complement of reads.SignalGrant, so a
			// consumer inside it is not hidden however the page was bound.
			if len(reads.SignalGrant) > 0 && !slices.Contains(reads.SignalGrant, consumerRepoID) {
				hidden[entityID] = struct{}{}
			}
			if len(reads.PageRepositoryIDs) > 0 && !slices.Contains(reads.PageRepositoryIDs, consumerRepoID) {
				continue
			}
			evidence[entityID] = append(evidence[entityID], row)
		}
	}
	return evidence, hidden, nil
}

func crossRepoDeadCodeGrantConsumerRow(consumerRepoID string, entityID string) deadcode.CrossRepoDeadCodeEvidence {
	return deadcode.CrossRepoDeadCodeEvidence{
		ConsumerRepoID:   consumerRepoID,
		ConsumerRepoName: consumerRepoID,
		ConsumerEntityID: consumerRepoID + "#caller",
		RelationshipType: "CALLS",
		EvidenceFamily:   "direct_code",
		Citation:         "code_reachability_rows:g1/" + consumerRepoID + "/caller/" + entityID,
		Confidence:       0.95,
		ConfidenceLabel:  "high",
		ResolutionMethod: "bounded_lookup",
		Depth:            1,
		GenerationID:     "g1",
		GenerationStatus: "active",
	}
}

// newCodeGrantRouteRequest builds an envelope-accepting POST request for one
// code route, carrying auth as the request's AuthContext when non-nil (nil
// means an unscoped shared-key caller). Copy of codequery's
// auth_scoped_code_topic_grant_test.go helper.
func newCodeGrantRouteRequest(t *testing.T, path string, body map[string]any, auth *queryauth.AuthContext) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal(body) error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	if auth != nil {
		req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), *auth))
	}
	return req
}

// deadCodeTestGrantFilter is codequery's codeGrantAccessFilter
// (repository_selector.go) as the same expression over exported leaves, for
// Analyzer-based tests that drive scan methods directly.
func deadCodeTestGrantFilter(ctx context.Context) deadcode.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilterFromContext(ctx).WithCanonicalScopeRepositories()
}

// deadCodeTestGrantScope is codequery's codeContentGrantScope
// (repository_selector.go) over the same expression, for the test candidate
// reader below.
func deadCodeTestGrantScope(ctx context.Context, repoID string) (allowed []string, blocked bool) {
	access := deadCodeTestGrantFilter(ctx)
	if access.Empty() {
		return nil, true
	}
	if !access.Scoped() {
		return nil, false
	}
	if repoID = strings.TrimSpace(repoID); repoID != "" {
		return nil, !access.AllowsRepositoryID(repoID)
	}
	return access.RepositorySearchIDs(), false
}

// deadCodeTestCandidateRows is codequery's pinned candidate-row reader
// (*CodeHandler).deadCodeCandidateRows (analyzer.go) bound to the test's own
// backends instead of the handler's, for Analyzer-based tests that drive scan
// methods directly.
func deadCodeTestCandidateRows(
	content deadcode.ContentStore,
	graph deadcode.GraphQuery,
) func(ctx context.Context, repoID, label, language string, limit, offset int) ([]map[string]any, error) {
	return func(ctx context.Context, repoID, label, language string, limit, offset int) ([]map[string]any, error) {
		allowed, blocked := deadCodeTestGrantScope(ctx, repoID)
		if blocked {
			return nil, nil
		}
		query := codeshaping.DeadCodeCandidateQuery{
			RepoID:               repoID,
			Label:                label,
			Language:             language,
			Limit:                limit,
			Offset:               offset,
			AllowedRepositoryIDs: allowed,
		}
		if store, ok := content.(codeshaping.DeadCodeCandidateContentStore); ok {
			return store.DeadCodeCandidateRows(ctx, query)
		}
		access := deadCodeTestGrantFilter(ctx)
		cypher := deadcode.BuildDeadCodeGraphCypherForLabel(repoID != "", label, language, access)
		return graph.Run(ctx, cypher, deadcode.DeadCodeGraphParams(repoID, language, limit, offset, access))
	}
}

// deadCodeTestIncomingEdges is codequery's pinned incoming-edge reader
// (*CodeHandler).deadCodeResultsWithGraphIncomingEdges (analyzer.go) bound to
// the test's graph backend, for Analyzer-based tests and the direct probe
// tests that pin its grant-projection semantics.
func deadCodeTestIncomingEdges(
	graph deadcode.GraphQuery,
) func(ctx context.Context, results []map[string]any, label string) (map[string]deadcode.DeadCodeIncomingEdge, error) {
	return func(ctx context.Context, results []map[string]any, label string) (map[string]deadcode.DeadCodeIncomingEdge, error) {
		entityIDs := deadcode.DeadCodeResultEntityIDs(results)
		incoming := make(map[string]deadcode.DeadCodeIncomingEdge)
		if len(entityIDs) == 0 {
			return incoming, nil
		}
		access := deadCodeTestGrantFilter(ctx)
		rows, err := graph.Run(
			ctx,
			deadcode.BuildDeadCodeScopedIncomingBatchProbeCypher(label, access),
			access.GraphParams(map[string]any{"entity_ids": entityIDs}),
		)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			entityID := strings.TrimSpace(querycontract.StringVal(row, "incoming_entity_id"))
			if entityID == "" {
				continue
			}
			// in_grant is projected only by the scoped statement. BoolVal reads an
			// absent column as false, so the unscoped caller -- whose statement has
			// no such column and whose every row is evidence -- must be answered
			// before the column is consulted at all.
			if access.Scoped() && !querycontract.BoolVal(row, "in_grant") {
				deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadcode.DeadCodeIncomingEdge{HiddenConsumer: true})
				continue
			}
			method := strings.TrimSpace(querycontract.StringVal(row, "resolution_method"))
			deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadcode.DeadCodeIncomingEdge{
				MaxConfidence: codeprovenance.Confidence(method),
				Method:        method,
			})
		}
		return incoming, nil
	}
}

// newDeadCodeTestAnalyzer binds an Analyzer the way codequery's per-call
// delegate does (*CodeHandler).deadAnalyzer, but onto the test's own
// backends, for tests that drive scan methods directly. Only the scan path's
// dependencies are wired; the HTTP-writer and selector fields stay nil
// because the scan methods never touch them.
func newDeadCodeTestAnalyzer(content deadcode.ContentStore, graph deadcode.GraphQuery) *deadcode.Analyzer {
	return deadcode.NewAnalyzer(deadcode.Dependencies{
		Content:         content,
		Graph:           graph,
		GrantFilter:     deadCodeTestGrantFilter,
		CandidateRows:   deadCodeTestCandidateRows(content, graph),
		IncomingEdges:   deadCodeTestIncomingEdges(graph),
		MergeMetadata:   search.MergeMetadata,
		ResultEntityIDs: deadcode.DeadCodeResultEntityIDs,
		NextCalls:       deadcode.InvestigationNextCalls,
	})
}
