// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFactStoreListActivePackageManifestDependencyFactsUsesActiveGenerations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-dep-1",
					"repository:repo-1",
					"generation-1",
					"content_entity",
					"content_entity:dep-1",
					"1.0.0",
					"git",
					int64(0),
					"unknown",
					"git",
					"content_entity:dep-1",
					"file:///repo/path",
					"dep-1",
					time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"entity_type":"Variable","entity_name":"left-pad","entity_metadata":{"config_kind":"dependency","package_manager":"npm"}}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActivePackageManifestDependencyFacts(
		context.Background(),
		[]string{"npm"},
		[]string{"left-pad"},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActivePackageManifestDependencyFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "content_entity"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = 'content_entity'",
		"fact.source_system = 'git'",
		"fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])",
		"fact.payload->>'entity_name' = ANY($2::text[])",
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $5",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

// TestFactStoreListActivePackageManifestDependencyFactsExcludesTombstones is the
// regression guard for issue #1927: a manifest-dependency content_entity fact
// tombstoned within a still-active generation must not be returned as live to
// reducer correlation consumers. The active-generation join keeps tombstoned
// facts visible (a tombstone supersedes within the same generation), so the
// read model must filter them out with an explicit predicate, matching every
// sibling active source-local reader. Two of the three consumers
// (package_source_correlation_handler.go, security_alert_reconciliation_handler.go)
// do not filter IsTombstone themselves, so the guard belongs in the query.
func TestFactStoreListActivePackageManifestDependencyFactsExcludesTombstones(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewFactStore(db)

	if _, err := store.ListActivePackageManifestDependencyFacts(
		context.Background(),
		[]string{"npm"},
		[]string{"left-pad"},
	); err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "fact.is_tombstone = FALSE") {
		t.Fatalf("query must exclude tombstoned manifest dependency facts via %q:\n%s",
			"fact.is_tombstone = FALSE", query)
	}
}
