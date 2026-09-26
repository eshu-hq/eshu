// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// The dead-code budget fixtures reproduce the measured production shape from
// #7168 at fixture size: a repository with far more candidates than any
// response can carry, so the default `limit` alone decides whether the reply
// fits the MCP dispatch budget. Rows use the post-#7167 metadata shape (no
// body fingerprint keys). The docstring is echoed by the row's metadata and by
// each language semantics block, so a short docstring is enough to give each
// row roughly the measured 1 KB+ cost. The fixture is sized so a default-limit
// reply lands near three quarters of the budget and a limit-100 reply is far
// over it; the old-default test below pins that the fixture can tell the two
// apart.
const (
	deadCodeBudgetProducerRepoID = "repo-producer"
	deadCodeBudgetAmbiguousRows  = 400
	deadCodeBudgetSuppressedRows = 200
	deadCodeBudgetRowPadding     = 60
)

// deadCodeBudgetStore serves the candidate scan, the entity reads, and the
// cross-repo consumer evidence for both dead-code tools from one fixture.
type deadCodeBudgetStore struct {
	content.FakeDeadCodeContentStore
	rows []map[string]any
	// evidencePerEntity is how many ambiguous consumer_evidence items each
	// producer candidate carries, matching the two-item cross-repo rows.
	evidencePerEntity int
}

func (s *deadCodeBudgetStore) DeadCodeCandidateRows(
	_ context.Context,
	query codeshaping.DeadCodeCandidateQuery,
) ([]map[string]any, error) {
	if query.Label != "Function" || query.Offset >= len(s.rows) {
		return nil, nil
	}
	end := min(query.Offset+query.Limit, len(s.rows))
	return s.rows[query.Offset:end], nil
}

func (s *deadCodeBudgetStore) CrossRepoDeadCodeConsumerEvidence(
	_ context.Context,
	_ string,
	entityIDs []string,
	_ code.CrossRepoDeadCodeConsumerReads,
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, code.CrossRepoDeadCodeHiddenConsumers, error) {
	result := make(map[string][]deadcode.CrossRepoDeadCodeEvidence, len(entityIDs))
	for _, entityID := range entityIDs {
		for index := 0; index < s.evidencePerEntity; index++ {
			result[entityID] = append(result[entityID], deadcode.CrossRepoDeadCodeEvidence{
				ConsumerRepoID:   fmt.Sprintf("repo-consumer-%d", index),
				EvidenceFamily:   "package_module_repo",
				Citation:         "repository_relationships:repo-consumer->" + deadCodeBudgetProducerRepoID,
				Confidence:       0.5,
				ConfidenceLabel:  "low",
				ResolutionMethod: "repo_unique_name",
				Ambiguous:        true,
				NeedsEvidence:    true,
				Reason:           "ambiguous_consumer_ownership",
				GenerationID:     "gen-a",
				GenerationStatus: "active",
			})
		}
	}
	return result, code.CrossRepoDeadCodeHiddenConsumers{}, nil
}

// newDeadCodeBudgetStore builds the fixture: ambiguous TypeScript candidates
// interleaved with suppressed Python framework roots, so the modeled-root
// bucket fills as fast as the active buckets do.
func newDeadCodeBudgetStore(evidencePerEntity int) *deadCodeBudgetStore {
	entities := make(map[string]querycontract.EntityContent)
	rows := make([]map[string]any, 0, deadCodeBudgetAmbiguousRows+deadCodeBudgetSuppressedRows)
	padding := strings.Repeat("x", deadCodeBudgetRowPadding)
	addRow := func(entityID, language, path string, metadata map[string]any) {
		entities[entityID] = querycontract.EntityContent{
			EntityID:     entityID,
			RepoID:       deadCodeBudgetProducerRepoID,
			RelativePath: path,
			EntityType:   "Function",
			EntityName:   entityID,
			StartLine:    10,
			EndLine:      20,
			Language:     language,
			SourceCache:  "function " + entityID + "() {}",
			Metadata:     metadata,
		}
		rows = append(rows, map[string]any{
			"entity_id":  entityID,
			"name":       entityID,
			"labels":     []any{"Function"},
			"file_path":  path,
			"repo_id":    deadCodeBudgetProducerRepoID,
			"repo_name":  "payments-lib",
			"language":   language,
			"start_line": int64(10),
			"end_line":   int64(20),
		})
	}
	for index := 0; index < deadCodeBudgetAmbiguousRows; index++ {
		addRow(fmt.Sprintf("ts-%03d", index), "typescript", fmt.Sprintf("src/ts/file%03d.ts", index),
			map[string]any{"docstring": padding})
		if index < deadCodeBudgetSuppressedRows {
			addRow(fmt.Sprintf("py-%03d", index), "python", fmt.Sprintf("api/route%03d.py", index),
				map[string]any{
					"docstring":            padding,
					"dead_code_root_kinds": []string{"python.flask_route_decorator"},
				})
		}
	}
	return &deadCodeBudgetStore{
		FakeDeadCodeContentStore: content.FakeDeadCodeContentStore{
			FakePortContentStore: content.FakePortContentStore{
				Repositories: []querycontract.RepositoryCatalogEntry{
					{ID: deadCodeBudgetProducerRepoID, Name: "payments-lib"},
				},
			},
			Entities: entities,
		},
		rows:              rows,
		evidencePerEntity: evidencePerEntity,
	}
}

func deadCodeBudgetMux(store *deadCodeBudgetStore) http.Handler {
	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j:   graph.FakeGraphReader{},
		Content: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

// TestDeadCodeToolsDefaultResponseStaysWithinBudget dispatches each dead-code
// tool with no limit argument and requires the reply to fit the response
// budget. At the old default of 100 both replies exceeded it (#7168); the
// default must now bound the response by construction.
func TestDeadCodeToolsDefaultResponseStaysWithinBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tool              string
		args              map[string]any
		evidencePerEntity int
	}{
		{tool: "investigate_dead_code", args: map[string]any{"repo_id": deadCodeBudgetProducerRepoID}},
		{
			tool:              "find_cross_repo_dead_code",
			args:              map[string]any{"repo_id": deadCodeBudgetProducerRepoID},
			evidencePerEntity: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()

			result, err := dispatchTool(
				context.Background(),
				deadCodeBudgetMux(newDeadCodeBudgetStore(tt.evidencePerEntity)),
				tt.tool,
				tt.args,
				"",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err != nil {
				t.Fatalf("dispatchTool(%s) error = %v, want nil", tt.tool, err)
			}
			if result == nil {
				t.Fatalf("dispatchTool(%s) = nil, want a result", tt.tool)
			}
			if result.IsError {
				t.Fatalf("dispatchTool(%s) default-args reply is an error %#v, want a success within budget",
					tt.tool, result.Envelope.Error)
			}
			size := estimateResponseBytes(result)
			if size > defaultToolResponseByteBudget {
				t.Fatalf("%s default-args response = %d bytes, want <= %d", tt.tool, size, defaultToolResponseByteBudget)
			}
			t.Logf("%s default-args response_bytes=%d budget=%d", tt.tool, size, defaultToolResponseByteBudget)
		})
	}
}

// TestDeadCodeToolsOldDefaultFixtureExceedsBudget proves the fixture is
// representative: at the retired limit of 100 the same requests are over
// budget, so the passing default test above is a real bound and not a fixture
// too small to notice one.
func TestDeadCodeToolsOldDefaultFixtureExceedsBudget(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"investigate_dead_code", "find_cross_repo_dead_code"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()

			evidence := 0
			if tool == "find_cross_repo_dead_code" {
				evidence = 2
			}
			result, err := dispatchTool(
				context.Background(),
				deadCodeBudgetMux(newDeadCodeBudgetStore(evidence)),
				tool,
				map[string]any{"repo_id": deadCodeBudgetProducerRepoID, "limit": float64(100)},
				"",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err != nil {
				t.Fatalf("dispatchTool(%s) error = %v, want nil", tool, err)
			}
			if result == nil || !result.IsError || result.Envelope == nil || result.Envelope.Error == nil ||
				result.Envelope.Error.Code != errorCodeResponseOverBudget {
				t.Fatalf("%s limit=100 = %#v, want the canonical over-budget error", tool, result)
			}
		})
	}
}
