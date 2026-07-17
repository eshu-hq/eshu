// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestFactRecordSchemaIncludesCodeFlowRepoIndex proves the schema registers the
// partial index that makes the cumulative-active code-flow read (#5280)
// seekable by target repository instead of a residual heap filter over every
// scope's code-flow facts. The partial predicate must cover all three code-flow
// fact kinds the read queries and must NOT exclude tombstones (the read ranks
// retractions to pick the newest generation before dropping rn=1 tombstones).
func TestFactRecordSchemaIncludesCodeFlowRepoIndex(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact_records_code_flow_repo_idx",
		"((payload->>'repo_id'), scope_id, generation_id, fact_id)",
		"fact_kind IN ('code_taint_evidence', 'code_interproc_evidence', 'code_dataflow_function')",
	} {
		if !strings.Contains(factRecordSchemaSQL, want) {
			t.Fatalf("factRecordSchemaSQL missing %q", want)
		}
	}

	start := strings.Index(factRecordSchemaSQL, "fact_records_code_flow_repo_idx")
	if start < 0 {
		t.Fatal("code-flow repo index statement not found in schema")
	}
	idx := factRecordSchemaSQL[start:]
	if end := strings.Index(idx, ";"); end >= 0 {
		idx = idx[:end+1]
	}
	if strings.Contains(idx, "is_tombstone") {
		t.Fatalf("code-flow repo index must not filter is_tombstone (the read ranks tombstones): %s", idx)
	}
}
