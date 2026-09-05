// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"testing"
)

// The differential half of TestCrossRepoDeadCodeUngrantedConsumerProbeLive:
// every grant shape the shipped walk is required to answer the same way as the
// `NOT (repository_id = ANY($grant))` it replaces. The work and plan guards
// that cover what an answer cannot see are in the sibling _work_test.go and
// _plan_test.go files.

// runCrossRepoDeadCodeProbeGrantShapes drives the named grant shapes through
// the shipped probe and requires the reference statement's answer for each.
func runCrossRepoDeadCodeProbeGrantShapes(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	reader *ContentReader,
	page []string,
) {
	t.Helper()

	cases := []struct {
		name  string
		grant []string
		want  []string
	}{
		{name: "every consumer granted", grant: crossRepoDeadCodeProbeFanInRepositories},
		{
			name:  "hidden consumer below the smallest granted id",
			grant: []string{"repo-c", "repo-e", "repo-g", "repo-i"},
			want:  []string{"ent-busy", "ent-retained", "ent-spread"},
		},
		{
			name:  "hidden consumer between two granted ids",
			grant: []string{"repo-a", "repo-c", "repo-g", "repo-i"},
			want:  []string{"ent-busy", "ent-middle", "ent-retained", "ent-spread"},
		},
		{
			name:  "hidden consumer above the largest granted id",
			grant: []string{"repo-a", "repo-c", "repo-e", "repo-g"},
			want:  []string{"ent-busy", "ent-retained", "ent-spread"},
		},
		{
			// One granted id makes both outer ranges and no interior one, and
			// ent-middle -- whose only consumer is that id -- must stay unflagged
			// while ent-spread, which has consumers on both sides of it, does not.
			name:  "single-element grant",
			grant: []string{"repo-e"},
			want:  []string{"ent-busy", "ent-retained", "ent-spread"},
		},
		{
			name:  "grant wider than the corpus",
			grant: []string{"repo-a", "repo-c", "repo-e", "repo-g", "repo-i", "repo-producer", "repo-z"},
		},
		{
			name:  "grant disjoint from every consumer",
			grant: []string{"repo-b", "repo-d"},
			want:  []string{"ent-busy", "ent-middle", "ent-retained", "ent-spread"},
		},
		{
			name:  "grant naming only the producer repository",
			grant: []string{"repo-producer"},
			want:  []string{"ent-busy", "ent-middle", "ent-retained", "ent-spread"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(ctx, "repo-producer", page, testCase.grant)
			if err != nil {
				t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
			}
			got := make([]string, 0, len(hidden))
			for entityID := range hidden {
				got = append(got, entityID)
			}
			sort.Strings(got)
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("hidden = %#v, want %#v", got, testCase.want)
			}
			// The range rewrite is only worth anything if it agrees with the
			// NOT IN it replaced. Ask the same question the slow way and
			// require an identical answer, so a bound that drifts by one
			// operator fails here rather than in a tenant's results.
			reference := crossRepoDeadCodeProbeReference(ctx, t, db, "repo-producer", page, testCase.grant)
			if !slices.Equal(got, reference) {
				t.Fatalf("probe = %#v, NOT IN reference = %#v; the ranges are not the complement of the grant", got, reference)
			}
		})
	}
}

// runCrossRepoDeadCodeProbeUngrantedScopeWalk covers the ungranted side of the
// scope rule, where the walk has to keep stepping scope by scope.
func runCrossRepoDeadCodeProbeUngrantedScopeWalk(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	reader *ContentReader,
) {
	t.Helper()

	t.Run("an ungranted repository's scopes are walked until one is live", func(t *testing.T) {
		// The other half of the same rule, and the reason the skip is
		// conditional rather than unconditional. A repository the caller cannot
		// see hides a consumer if ANY of its scopes has a live row, so its
		// scopes have to be walked -- skipping to the next repository from an
		// ungranted pair would answer "nothing hidden" for
		// ent-scopes-ungranted, whose live row sits in the 51st scope of its
		// only consumer repository, behind 50 whose rows are all superseded.
		// ent-scopes-ungranted-stale is the same shape with nothing live in any
		// scope, so the walk exhausts the repository and correctly finds
		// nothing.
		hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(
			ctx, "repo-producer",
			[]string{"ent-scopes-granted", "ent-scopes-ungranted", "ent-scopes-ungranted-stale"},
			crossRepoDeadCodeProbeFanInRepositories,
		)
		if err != nil {
			t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
		}
		got := make([]string, 0, len(hidden))
		for entityID := range hidden {
			got = append(got, entityID)
		}
		sort.Strings(got)
		want := []string{"ent-scopes-granted", "ent-scopes-ungranted"}
		if !slices.Equal(got, want) {
			t.Fatalf("hidden = %#v, want %#v", got, want)
		}
		reference := crossRepoDeadCodeProbeReference(
			ctx, t, db, "repo-producer",
			[]string{"ent-scopes-granted", "ent-scopes-ungranted", "ent-scopes-ungranted-stale"},
			crossRepoDeadCodeProbeFanInRepositories,
		)
		if !slices.Equal(got, reference) {
			t.Fatalf("probe = %#v, NOT IN reference = %#v; the scope walk is not the complement of the grant", got, reference)
		}
	})
}

// runCrossRepoDeadCodeProbeBroadGrant covers the grant sizes that exposed the
// shape this walk replaced.
func runCrossRepoDeadCodeProbeBroadGrant(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	reader *ContentReader,
	page []string,
) {
	t.Helper()

	// The read this walk replaced cost one index probe per granted repository
	// per producer entity, so a caller with a broad grant paid for the grant
	// rather than for the answer. These grants are the sizes that exposed it:
	// at 500 granted repositories the old shape took 633 ms on the corpus-scale
	// seed against 5.0 ms for this one. Correctness at those sizes is what is
	// asserted here; the timings are in the evidence doc.
	t.Run("a broad grant changes the answer for no entity", func(t *testing.T) {
		broad := make([]string, 0, 500)
		broad = append(broad, crossRepoDeadCodeProbeFanInRepositories...)
		for i := 0; len(broad) < 500; i++ {
			candidate := fmt.Sprintf("repo-pad%04d", i)
			if !slices.Contains(crossRepoDeadCodeProbeFanInRepositories, candidate) {
				broad = append(broad, candidate)
			}
		}
		for _, testCase := range []struct {
			name  string
			grant []string
			want  []string
		}{
			{name: "500 granted, every consumer among them", grant: broad},
			{
				name:  "500 granted, one consumer left out",
				grant: append(append([]string(nil), broad[:2]...), broad[3:]...),
				want:  []string{"ent-busy", "ent-middle", "ent-retained", "ent-spread"},
			},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(ctx, "repo-producer", page, testCase.grant)
				if err != nil {
					t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
				}
				got := make([]string, 0, len(hidden))
				for entityID := range hidden {
					got = append(got, entityID)
				}
				sort.Strings(got)
				if !slices.Equal(got, testCase.want) {
					t.Fatalf("hidden = %#v, want %#v", got, testCase.want)
				}
				reference := crossRepoDeadCodeProbeReference(ctx, t, db, "repo-producer", page, testCase.grant)
				if !slices.Equal(got, reference) {
					t.Fatalf("probe = %#v, NOT IN reference = %#v", got, reference)
				}
			})
		}
	})
}

// crossRepoDeadCodeProbeReference answers the probe's question with the
// unseekable predicate the probe replaced.
func crossRepoDeadCodeProbeReference(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
SELECT DISTINCT row.entity_id
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.entity_id = ANY($2)
  AND row.repository_id <> $1
  AND row.depth > 0
  AND NOT (row.repository_id = ANY($3))
ORDER BY row.entity_id`,
		producerRepoID,
		crossRepoDeadCodeProbeTextArray(entityIDs),
		crossRepoDeadCodeProbeTextArray(grantRepositoryIDs),
	)
	if err != nil {
		t.Fatalf("reference query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	entities := make([]string, 0, len(entityIDs))
	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			t.Fatalf("scan reference row: %v", err)
		}
		entities = append(entities, entityID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reference rows: %v", err)
	}
	return entities
}
