// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestDocumentationFindingsIndexesCoverUnfilteredAndSelectiveLists(t *testing.T) {
	t.Parallel()

	orderDDL := indexStatementForTest(
		t,
		documentationFactRecordReadIndexesSQL,
		"fact_records_documentation_findings_read_idx",
	)
	if got := documentationFindingIndexKeysForTest(orderDDL); got != "observed_at desc, fact_id desc" {
		t.Fatalf("unfiltered findings index keys = %q, want order-first list keys", got)
	}

	filterDDL := indexStatementForTest(
		t,
		documentationFactRecordReadIndexesSQL,
		"fact_records_documentation_findings_filter_idx",
	)
	wantFilterPrefix := "(payload->>'finding_type'), (payload->>'source_id'), (payload->>'document_id'), " +
		"(payload->>'status'), (payload->>'truth_level'), (payload->>'freshness_state'), " +
		"observed_at desc, fact_id desc"
	if got := documentationFindingIndexKeysForTest(filterDDL); got != wantFilterPrefix {
		t.Fatalf("selective findings index keys = %q, want %q", got, wantFilterPrefix)
	}

	definitions := BootstrapDefinitions()
	for _, path := range []string{
		"go/internal/storage/postgres/migrations/065_create_documentation_findings_read_idx.sql",
		"go/internal/storage/postgres/migrations/066_create_documentation_findings_filter_idx.sql",
	} {
		if _, ok := definitionByPathForTest(definitions, path); !ok {
			t.Errorf("documentation findings migration %q is missing", path)
		}
	}
}

func documentationFindingIndexKeysForTest(definition string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
	marker := " on fact_records ("
	start := strings.Index(normalized, marker)
	if start < 0 {
		return normalized
	}
	remaining := normalized[start+len(marker):]
	end := strings.Index(remaining, ") where ")
	if end < 0 {
		return remaining
	}
	return remaining[:end]
}
