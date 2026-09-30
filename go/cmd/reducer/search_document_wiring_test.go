// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestSearchDocumentHandlersWireGenerationCheck fails when the production
// search-document wiring leaves the generation check unset (#7458): an unset
// check makes every eshu_search_document Handle call fail, so the wiring must
// supply the Postgres freshness check next to the loader and writer.
func TestSearchDocumentHandlersWireGenerationCheck(t *testing.T) {
	t.Parallel()

	database := &fakeReducerDB{}
	handlers := buildReducerSearchDocumentHandlers(database, (*telemetry.Instruments)(nil), nil, nil)
	if handlers.EshuSearchDocumentGenerationCheck == nil {
		t.Fatal("EshuSearchDocumentGenerationCheck is nil; the handler would reject every intent")
	}
	current, err := handlers.EshuSearchDocumentGenerationCheck(context.Background(), "scope-123", "generation-456")
	if err != nil {
		t.Fatalf("generation check error = %v", err)
	}
	if !current {
		t.Fatal("generation check = false, want true for the generation the database reports active")
	}
}
