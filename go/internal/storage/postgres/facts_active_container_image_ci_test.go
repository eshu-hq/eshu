// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestFactStoreListActiveContainerImageCIFactsUsesActiveGenerations is the
// #5810 Postgres-side regression, mirroring
// TestFactStoreListActiveContainerImageSLSAFactsUsesActiveGenerations: the
// cross-scope CI loader must resolve active-generation ci.run/ci.artifact
// facts regardless of which scope they were written in, so a
// container_image_identity refresh triggered from a different scope (the
// repository whose Dockerfile owns the DERIVED_FROM projection) can still
// join them.
func TestFactStoreListActiveContainerImageCIFactsUsesActiveGenerations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"ci.run:run-1",
					"ci_cd_run:github_actions:team-api",
					"generation-1",
					"ci.run",
					"ci_cd_run:github_actions:team-api:run-1",
					"1.0.0",
					"ci_cd_run",
					int64(1),
					"reported",
					"ci_cd_run",
					"run-1",
					"",
					"",
					time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"provider":"github_actions","run_id":"run-1","repository_id":"repository:team-api"}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveContainerImageCIFacts(context.Background(), "repository:team-api")
	if err != nil {
		t.Fatalf("ListActiveContainerImageCIFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActiveContainerImageCIFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "ci.run"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact_kind = 'ci.run'",
		"fact.fact_kind = 'ci.artifact'",
		"source_system = 'ci_cd_run'",
		"payload->>'artifact_type' = 'container_image'",
		"payload->>'repository_id' = $1",
		"ORDER BY observed_at ASC, fact_id ASC",
		"LIMIT $4",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[0], "repository:team-api"; got != want {
		t.Fatalf("query $1 arg = %#v, want %q (the owner repository)", got, want)
	}
}

// TestFactStoreListActiveContainerImageCIFactsExcludesTombstones mirrors
// TestFactStoreListActiveContainerImageSLSAFactsExcludesTombstones: a ci.run
// or ci.artifact fact tombstoned within a still-active generation (superseded
// by a newer run/artifact observation) must not be returned as live evidence
// the DERIVED_FROM CI tier could trust.
func TestFactStoreListActiveContainerImageCIFactsExcludesTombstones(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewFactStore(db)

	if _, err := store.ListActiveContainerImageCIFacts(context.Background(), "repository:r_owner"); err != nil {
		t.Fatalf("ListActiveContainerImageCIFacts() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "is_tombstone = FALSE") {
		t.Fatalf("query must exclude tombstoned CI facts via %q:\n%s",
			"is_tombstone = FALSE", query)
	}
}

// TestFactStoreListActiveContainerImageCIFactsNarrowsArtifactType proves the
// load-bearing artifact_type narrowing (#5810): the query only admits
// ci.artifact rows whose payload artifact_type is container_image, matching
// what the reducer's addCICDArtifactImageReference
// (container_image_identity_typed_evidence.go) actually consumes. Non-image
// artifacts (coverage reports, SBOM bundles) are the bulk of ci.artifact
// volume and would otherwise inflate this cross-scope load with rows the
// reducer immediately discards.
func TestFactStoreListActiveContainerImageCIFactsNarrowsArtifactType(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewFactStore(db)

	if _, err := store.ListActiveContainerImageCIFacts(context.Background(), "repository:r_owner"); err != nil {
		t.Fatalf("ListActiveContainerImageCIFacts() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "fact.fact_kind = 'ci.artifact' AND fact.source_system = 'ci_cd_run'") ||
		!strings.Contains(query, "AND fact.payload->>'artifact_type' = 'container_image'") {
		t.Fatalf("query must narrow ci.artifact to artifact_type = container_image:\n%s", query)
	}
}

// TestFactStoreListActiveContainerImageCIFactsRequiresOwnerRepository is the
// #5810 P1 follow-up regression: an empty ownerRepositoryID must short-circuit
// with no query at all rather than fall back to an unbounded, unowned load.
// The reducer's Handle path (loadActiveContainerImageCIFacts,
// container_image_identity_ci_loader.go) already skips calling this loader
// for a non-repository scope, so a blank owner reaching here can only be a
// caller bug -- returning empty is the safe failure mode, not silently
// reverting to the platform-wide scan this change exists to remove.
func TestFactStoreListActiveContainerImageCIFactsRequiresOwnerRepository(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveContainerImageCIFacts(context.Background(), "")
	if err != nil {
		t.Fatalf("ListActiveContainerImageCIFacts(\"\") error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("ListActiveContainerImageCIFacts(\"\") len = %d, want 0", len(loaded))
	}
	if got := len(db.queries); got != 0 {
		t.Fatalf("query count = %d, want 0 (an empty owner must not issue the unbounded query)", got)
	}
}

// TestFactStoreListActiveContainerImageCIFactsPaginates mirrors the
// cross-scope loader pagination contract every ListActiveContainerImage*
// loader shares (facts_active_container_image_slsa.go,
// facts_active_repository.go): a full page advances the keyset cursor to the
// last row's (observed_at, fact_id) and issues another page request, so an
// active-fact volume larger than one page is not silently truncated.
func TestFactStoreListActiveContainerImageCIFactsPaginates(t *testing.T) {
	t.Parallel()

	firstPage := make([][]any, listFactsByKindPageSize)
	baseTime := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	for i := range firstPage {
		factID := fmt.Sprintf("ci.run:run-%04d", i)
		firstPage[i] = []any{
			factID,
			"ci_cd_run:github_actions:team-api",
			"generation-1",
			"ci.run",
			"stable-" + factID,
			"1.0.0",
			"ci_cd_run",
			int64(1),
			"reported",
			"ci_cd_run",
			factID,
			"",
			"",
			baseTime.Add(time.Duration(i) * time.Second),
			false,
			[]byte(`{"provider":"github_actions","run_id":"` + factID + `"}`),
		}
	}
	secondPage := [][]any{{
		"ci.run:run-last",
		"ci_cd_run:github_actions:team-api",
		"generation-1",
		"ci.run",
		"stable-last",
		"1.0.0",
		"ci_cd_run",
		int64(1),
		"reported",
		"ci_cd_run",
		"run-last",
		"",
		"",
		baseTime.Add(time.Duration(len(firstPage)) * time.Second),
		false,
		[]byte(`{"provider":"github_actions","run_id":"run-last"}`),
	}}

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: firstPage},
			{rows: secondPage},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveContainerImageCIFacts(context.Background(), "repository:r_owner")
	if err != nil {
		t.Fatalf("ListActiveContainerImageCIFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), listFactsByKindPageSize+1; got != want {
		t.Fatalf("ListActiveContainerImageCIFacts() len = %d, want %d (paginated across two pages)", got, want)
	}
	if got := len(db.queries); got != 2 {
		t.Fatalf("query count = %d, want 2 (a full first page must request a second)", got)
	}
	if got, want := db.queries[1].args[0], "repository:r_owner"; got != want {
		t.Fatalf("second page $1 arg = %#v, want %q (the owner must be re-sent on every page)", got, want)
	}
}

// TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL locks
// the partial-index WHERE predicate for
// fact_records_active_container_image_ci_idx (migration 081) to the Go
// listActiveContainerImageCIFactsFilterSQL const
// (facts_active_container_image_ci.go), in the spirit of
// TestIdentityEpochIndexPredicateMatchesIdentityFactFilter
// (identity_epoch_cache_contract_test.go). listActiveContainerImageCIFactsQuery
// no longer issues this combined two-arm predicate as a single scan (#5810 P1
// follow-up splits it into two independently-optimized UNION ALL branches --
// see that constant's doc comment), so this test compares the migration's
// predicate directly against the Go constant text instead of extracting it
// out of the live query: migration 081's index stays valid and shipped even
// though this one consumer no longer drives its access path, and the
// predicate pairing must still not silently drift.
func TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL(t *testing.T) {
	t.Parallel()

	migrationSQL, err := os.ReadFile("migrations/081_fact_records_active_container_image_ci_idx.sql")
	if err != nil {
		t.Fatalf("read migration 081: %v", err)
	}

	migrationFilter := normalizeIdentityFilterPredicate(extractIdentityFilter(string(migrationSQL)))
	goFilter := normalizeIdentityFilterPredicate(listActiveContainerImageCIFactsFilterSQL)

	if migrationFilter != goFilter {
		t.Fatalf(
			"live container-image-CI index predicate drifted from listActiveContainerImageCIFactsFilterSQL:\nmigration: %s\ngo:        %s",
			migrationFilter, goFilter,
		)
	}

	for _, want := range []string{
		"fact_kind = 'ci.run'",
		"fact_kind = 'ci.artifact'",
		"source_system = 'ci_cd_run'",
		"payload->>'artifact_type' = 'container_image'",
	} {
		if !strings.Contains(migrationFilter, want) {
			t.Fatalf("live container-image-CI index predicate missing %q", want)
		}
		if !strings.Contains(goFilter, want) {
			t.Fatalf("listActiveContainerImageCIFactsFilterSQL missing %q", want)
		}
	}
}

// TestFactRecordsActiveContainerImageCIRunRepositoryIndexPredicateMatchesFilterSQL
// is the #5810 P1 follow-up sibling of
// TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL: it
// locks migration 082's WHERE predicate
// (fact_records_active_container_image_ci_run_repository_idx) to the Go
// listActiveContainerImageCIRunRepositoryFilterSQL const, the predicate that
// actually drives the owner-scoped query's ci.run branch today. If these two
// drift, migration 082 silently stops covering the load query and the
// owner-scoped lookup falls back to an unindexed scan -- the exact
// perf-regression-with-no-functional-failure risk
// TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL guards
// for migration 081.
func TestFactRecordsActiveContainerImageCIRunRepositoryIndexPredicateMatchesFilterSQL(t *testing.T) {
	t.Parallel()

	migrationSQL, err := os.ReadFile("migrations/082_fact_records_active_container_image_ci_run_repository_idx.sql")
	if err != nil {
		t.Fatalf("read migration 082: %v", err)
	}

	migrationFilter := normalizeIdentityFilterPredicate(extractWhereClause(string(migrationSQL)))
	goFilter := normalizeIdentityFilterPredicate(listActiveContainerImageCIRunRepositoryFilterSQL)

	if migrationFilter != goFilter {
		t.Fatalf(
			"live container-image-CI-run-repository index predicate drifted from listActiveContainerImageCIRunRepositoryFilterSQL:\nmigration: %s\ngo:        %s",
			migrationFilter, goFilter,
		)
	}

	for _, want := range []string{
		"fact_kind = 'ci.run'",
		"source_system = 'ci_cd_run'",
		"is_tombstone = FALSE",
	} {
		if !strings.Contains(migrationFilter, want) {
			t.Fatalf("live container-image-CI-run-repository index predicate missing %q", want)
		}
		if !strings.Contains(goFilter, want) {
			t.Fatalf("listActiveContainerImageCIRunRepositoryFilterSQL missing %q", want)
		}
	}
}

// extractWhereClause returns the plain (non-parenthesized) WHERE predicate
// trailing a CREATE INDEX statement, for migration 082's un-parenthesized
// AND-chain shape -- extractIdentityFilter (identity_epoch_cache_fingerprint_test.go)
// requires the predicate to open with '(' immediately, which migration 082's
// simple three-clause AND chain does not.
func extractWhereClause(sql string) string {
	whereIdx := strings.Index(sql, "WHERE")
	if whereIdx < 0 {
		panic("cannot find WHERE in migration SQL: " + sql)
	}
	rest := strings.TrimSpace(sql[whereIdx+5:])
	return strings.TrimSuffix(strings.TrimSpace(rest), ";")
}
