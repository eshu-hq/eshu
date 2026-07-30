// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFactStoreListsActiveContainerImageIdentityWarningsSeparately(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"warning-tag-list-truncated",
				"oci-registry://registry.example.com/team/api",
				"generation-oci",
				"oci_registry.warning",
				"warning:tag-list-truncated",
				"1.0.0",
				"oci_registry",
				int64(11),
				"reported",
				"oci_registry",
				"warning:tag-list-truncated",
				"",
				"tag_list_truncated",
				observedAt,
				false,
				[]byte(`{"repository_id":"oci-registry://registry.example.com/team/api","warning_code":"tag_list_truncated","warning_key":"tag_list_truncated","digest":""}`),
			}},
		}},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveContainerImageIdentityWarnings(context.Background())
	if err != nil {
		t.Fatalf("ListActiveContainerImageIdentityWarnings() error = %v", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActiveContainerImageIdentityWarnings() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "oci_registry.warning"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := loaded[0].Payload["warning_code"], "tag_list_truncated"; got != want {
		t.Fatalf("warning_code = %v, want %q", got, want)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"fact.fact_kind = 'oci_registry.warning'",
		"fact.source_system = 'oci_registry'",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $3",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("warning query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(identityFactFilterSQL, "'oci_registry.warning'") {
		t.Fatalf("cached identityFactFilterSQL must not absorb warning facts:\n%s", identityFactFilterSQL)
	}
}

func TestActiveContainerImageIdentityWarningIndexMatchesLiveQuery(t *testing.T) {
	t.Parallel()

	migrationSQL, err := os.ReadFile("migrations/087_fact_records_active_oci_warning_idx.sql")
	if err != nil {
		t.Fatalf("read migration 087: %v", err)
	}
	for name, sqlText := range map[string]string{
		"migration": string(migrationSQL),
		"bootstrap": factRecordSchemaSQL,
		"query":     listActiveContainerImageIdentityWarningsQuery,
	} {
		for _, want := range []string{
			"fact_kind = 'oci_registry.warning'",
			"source_system = 'oci_registry'",
			"is_tombstone = FALSE",
			"observed_at ASC",
			"fact_id ASC",
			"scope_id",
			"generation_id",
		} {
			if !strings.Contains(sqlText, want) {
				t.Fatalf("%s warning-index contract missing %q", name, want)
			}
		}
	}
	if !strings.Contains(
		factRecordSchemaSQL,
		"fact_records_active_oci_warning_idx",
	) {
		t.Fatal("bootstrap schema missing fact_records_active_oci_warning_idx")
	}
}
