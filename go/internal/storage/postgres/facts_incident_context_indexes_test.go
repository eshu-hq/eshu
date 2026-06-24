// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestFactRecordSchemaIncludesIncidentContextSourceRecordFallbackIndex(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact_records_incident_context_record_source_record_idx",
		"source_record_id",
		"WHERE fact_kind = 'incident.record'",
		"is_tombstone = FALSE",
	} {
		if !strings.Contains(factRecordSchemaSQL, want) {
			t.Fatalf("factRecordSchemaSQL missing %q", want)
		}
	}
}
