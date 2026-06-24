// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsBoundEshuSearchIndexTermKeys(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_index" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("eshu_search_index definition missing")
	}
	for _, want := range []string{
		"term_key TEXT NOT NULL",
		"PRIMARY KEY (scope_id, generation_id, term_key, document_id)",
		"ON eshu_search_index_terms (scope_id, generation_id, term_key)",
	} {
		if !strings.Contains(marker.SQL, want) {
			t.Fatalf("eshu_search_index SQL missing %q", want)
		}
	}
	if strings.Contains(marker.SQL, "PRIMARY KEY (scope_id, generation_id, term, document_id)") {
		t.Fatal("eshu_search_index terms still key raw term text")
	}
}
