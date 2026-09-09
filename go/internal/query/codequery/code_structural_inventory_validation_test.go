// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// Structural-inventory handler and request validation proofs. These stay in
// codequery: they exercise the moved CodeHandler and StructuralInventoryRequest
// through fakes and pure validation, with no root-owned ContentReader SQL or
// Where-builder calls. Split from code_structural_inventory_test.go at the
// lane-A move (#6060); the SQL-backed inventory proofs live in package query
// under the same basename.

func TestCodeHandlerStructuralInventoryRejectsInvalidBounds(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: querytestutil.FakePortContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/structure/inventory",
		bytes.NewBufferString(`{"inventory_kind":"entity","offset":10001}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "offset must be <= 10000") {
		t.Fatalf("body = %s, want offset bound error", w.Body.String())
	}
}

func TestStructuralInventoryValidationRejectsOverMaxLimit(t *testing.T) {
	t.Parallel()

	err := (StructuralInventoryRequest{
		RepoID:        "repo-1",
		InventoryKind: "entity",
		Limit:         structuralInventoryMaxLimit + 1,
	}).validate()

	if err == nil {
		t.Fatal("validate() error = nil, want limit bound error")
	}
	if got, want := err.Error(), "limit must be <= 200"; got != want {
		t.Fatalf("validate() error = %q, want %q", got, want)
	}
}

func TestStructuralInventoryValidationRequiresScope(t *testing.T) {
	t.Parallel()

	err := (StructuralInventoryRequest{InventoryKind: "super_call"}).validate()

	if err == nil {
		t.Fatal("validate() error = nil, want scope error")
	}
	if got, want := err.Error(), "one of repo_id, file_path, language, entity_kind, or symbol is required"; got != want {
		t.Fatalf("validate() error = %q, want %q", got, want)
	}
}

func TestStructuralInventoryValidationRejectsNonFunctionFileCounts(t *testing.T) {
	t.Parallel()

	err := (StructuralInventoryRequest{
		RepoID:        "repo-1",
		InventoryKind: "function_count_by_file",
		EntityKind:    "class",
	}).validate()

	if err == nil {
		t.Fatal("validate() error = nil, want entity_kind error")
	}
	if got, want := err.Error(), "entity_kind must be function for function_count_by_file inventory"; got != want {
		t.Fatalf("validate() error = %q, want %q", got, want)
	}
}
