// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// listRepositoriesBudgetMux serves the real repository list handler over a
// fake content catalog with n repositories, so the size proof exercises the
// production row shape instead of a synthetic body.
func listRepositoriesBudgetMux(n int) http.Handler {
	entries := make([]querycontract.RepositoryCatalogEntry, 0, n)
	for i := 0; i < n; i++ {
		name := "service-number-"
		if i < 100 {
			name += "0"
		}
		if i < 10 {
			name += "0"
		}
		entries = append(entries, querycontract.RepositoryCatalogEntry{
			ID:       "repository:repo-" + itoa(i),
			Name:     name + itoa(i),
			RepoSlug: "acme/service-number-" + itoa(i),
		})
	}
	mux := http.NewServeMux()
	(&repository.Handler{
		Content: content.FakePortContentStore{Repositories: entries},
		Profile: querycontract.ProfileLocalAuthoritative,
	}).Mount(mux)
	return mux
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// dispatchListRepositoriesShaped dispatches list_indexed_repositories at
// limit=500 through the real dispatch path under one result shape.
func dispatchListRepositoriesShaped(t *testing.T, mode resultshape.Mode, handler http.Handler) *dispatchResult {
	t.Helper()
	ctx := resultshape.WithOverride(context.Background(), mode)
	result, err := dispatchToolWithOptions(
		ctx, handler, "list_indexed_repositories",
		map[string]any{"limit": float64(500)},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		dispatchOptions{responseByteBudget: defaultToolResponseByteBudget},
	)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions(list_indexed_repositories) error = %v, want nil", err)
	}
	return result
}

// TestListIndexedRepositoriesLimit500CompactFitsBothShapes is the size
// acceptance for #10603: a limit=500 compact page (the MCP default when the
// caller sends no projection) fits the 256 KiB budget in dual and in
// structured, and the core handler body fits the production first cap.
// Recorded bytes land in the PR.
func TestListIndexedRepositoriesLimit500CompactFitsBothShapes(t *testing.T) {
	t.Parallel()

	handler := listRepositoriesBudgetMux(500)
	for _, mode := range []resultshape.Mode{resultshape.Dual, resultshape.Structured} {
		result := dispatchListRepositoriesShaped(t, mode, handler)
		if result.IsError || result.Envelope == nil || result.Envelope.Error != nil {
			code := ""
			if result.Envelope != nil && result.Envelope.Error != nil {
				code = string(result.Envelope.Error.Code)
			}
			t.Fatalf("shape %v IsError=%v code=%q, want a success within budget", mode, result.IsError, code)
		}
		size := estimateResponseBytes(result)
		t.Logf("limit=500 compact shape=%v response_bytes=%d budget=%d", mode, size, defaultToolResponseByteBudget)
		if size > defaultToolResponseByteBudget {
			t.Fatalf("shape %v response = %d bytes, want <= %d", mode, size, defaultToolResponseByteBudget)
		}
		data, ok := result.Envelope.Data.(map[string]any)
		if !ok {
			t.Fatalf("shape %v envelope data type = %T, want map", mode, result.Envelope.Data)
		}
		if got, want := data["projection"], "compact"; got != want {
			t.Fatalf("shape %v projection = %#v, want %#v (MCP default)", mode, got, want)
		}
		if got, want := data["total"], float64(500); got != want {
			t.Fatalf("shape %v total = %#v, want %#v", mode, got, want)
		}
	}
}
