// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestFactRecordSchemaIncludesIncidentWorkItemIndexes(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact_records_work_item_external_link_url_idx",
		"fact_kind = 'work_item.external_link'",
		"(payload->>'url')",
		"fact_records_work_item_record_key_idx",
		"fact_kind = 'work_item.record'",
		"(payload->>'work_item_key')",
	} {
		if !strings.Contains(factRecordSchemaSQL, want) {
			t.Fatalf("factRecordSchemaSQL missing %q", want)
		}
	}
}
