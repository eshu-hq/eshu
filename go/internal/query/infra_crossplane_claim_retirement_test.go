// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// TestSearchInfraResourcesCrossplaneCategoryOmitsDeadClaimLabel guards issue
// #5478: a Crossplane Claim has been edge-only since #5347 (it stays a
// K8sResource node; the SATISFIED_BY edge to its CrossplaneXRD is the
// classification), so no node ever carries the CrossplaneClaim label. The
// crossplane category must still search the live CrossplaneXRD/
// CrossplaneComposition labels, but must not emit a CrossplaneClaim branch
// that can only ever return zero rows.
func TestSearchInfraResourcesCrossplaneCategoryOmitsDeadClaimLabel(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"proof","category":"crossplane","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, fragment := range []string{"n:CrossplaneXRD", "n:CrossplaneComposition"} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}
	if strings.Contains(reader.lastCypher, "CrossplaneClaim") {
		t.Fatalf("cypher = %q, must not search the dead CrossplaneClaim label (issue #5478)", reader.lastCypher)
	}
	if labels, ok := infraCategoryLabels["crossplane"]; !ok {
		t.Fatal("infraCategoryLabels is missing the crossplane category")
	} else if got, want := labels, []string{"CrossplaneXRD", "CrossplaneComposition"}; !slices.Equal(got, want) {
		t.Fatalf("infraCategoryLabels[\"crossplane\"] = %#v, want %#v", got, want)
	}
	if slices.Contains(allInfraLabels, "CrossplaneClaim") {
		t.Fatal("allInfraLabels must not contain the dead CrossplaneClaim label (issue #5478)")
	}
}
