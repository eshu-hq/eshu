// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	codechain "github.com/eshu-hq/eshu/go/internal/query/codequery/chain"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestGetEntityContextAnchorsOnCodeEntityLabels pins the #7006 fix: the bare
// `MATCH (e) WHERE e.id = $entity_id` anchor scanned every node in the graph
// on every call -- proven live on ops-qa to blow the 10s bounded-read
// deadline. This route only ever resolves code entities (the graph-miss
// fallback reads the content store, not another graph label), so it must
// anchor on the same label disjunction the call-chain builder already uses.
// See docs/public/reference/cypher-performance.md.
func TestGetEntityContextAnchorsOnCodeEntityLabels(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			wantAnchor := "MATCH (e:" + codechain.AnchorLabelDisjunction + ") WHERE e.id = $entity_id"
			if !strings.Contains(cypher, wantAnchor) {
				t.Fatalf("cypher = %q, want it to contain the labeled anchor %q", cypher, wantAnchor)
			}
			if strings.Contains(cypher, "MATCH (e) WHERE e.id = $entity_id") {
				t.Fatalf("cypher still opens with the bare, unlabeled all-node-scan anchor:\n%s", cypher)
			}
			return map[string]any{
				"id":            "entity-a",
				"labels":        []any{"Function"},
				"name":          "HandlePayment",
				"relationships": []any{},
			}, nil
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}
