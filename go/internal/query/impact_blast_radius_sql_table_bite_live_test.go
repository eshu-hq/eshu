// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// impact_blast_radius_sql_table_bite_live_test.go is the standing bite proof
// for issue #6204.
//
// TestSQLTableBlastRadiusEveryBranchContributesLive is the repo's only
// dead-branch detector for the sql_table blast-radius UNION. Until now the only
// evidence that its detection actually bites was a manual experiment recorded in
// docs/internal/evidence/6182-blast-radius-gate-wiring.md: break the INDEXES
// branch, watch the gate fail naming it, revert. A recorded manual run is not a
// gate.
//
// The property that matters is that the missing list accumulates from the rows
// the query returned. A refactor that walked sqlBlastRadiusBranches() instead
// would keep every existing test green and gut the detection -- not
// hypothetical, since that is exactly what the test formerly named
// TestSQLTableBlastRadiusDetectsADeadBranchLive did, which is why #6201 renamed
// it to TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive.
//
// So this test seeds eight of the nine branches and asserts the ninth is
// reported missing BY NAME, through the same sqlBlastRadiusMissingBranches the
// nine-branch proof calls. It guards the production accumulation path, not a
// copy of it: gutting that helper to walk the fixture list turns this red while
// the nine-branch proof stays green.
//
// It uses sqlBlastRadiusBitePrefix, its own fixture prefix addressing its own
// shared table, so it cannot collide with the seeded set the nine-branch proof
// leaves in the same graph.
//
// Skills active: golang-engineering, cypher-query-rigor, eshu-diagnostic-rigor.

// TestSQLTableBlastRadiusReportsUnseededBranchMissingLive omits each UNION
// branch in turn, seeds the other eight, and asserts the omitted branch -- and
// only the omitted branch -- comes back in the missing list.
//
// Every branch gets a turn rather than one fixed branch because the cost is
// negligible: measured against the gate's own NornicDB image, all nine omission
// cycles complete in 156ms, against a blast-radius gate half that takes 3s warm
// and 14s cold. Covering one branch would prove the accumulation derives from
// rows just as well, and prove nothing about the other eight.
func TestSQLTableBlastRadiusReportsUnseededBranchMissingLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPLAY_TIER_LIVE")) != "1" {
		t.Skip("set ESHU_REPLAY_TIER_LIVE=1 to run the sql_table blast-radius bite proof against a real NornicDB")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	reader := sqlBlastRadiusLiveReader(ctx, t)

	branches := sqlBlastRadiusBranchesFor(sqlBlastRadiusBitePrefix)
	table := sqlBlastRadiusTableFor(sqlBlastRadiusBitePrefix)
	cleanup := func() { sqlBlastRadiusCleanup(t, reader, sqlBlastRadiusBitePrefix) }
	cleanup()
	t.Cleanup(cleanup)

	for omitted := range branches {
		// No t.Parallel: every subtest seeds and deletes the same prefix in one
		// shared graph, so they must run one at a time.
		t.Run(branches[omitted].Name, func(t *testing.T) {
			sqlBlastRadiusCleanup(t, reader, sqlBlastRadiusBitePrefix)

			// The CONTAINS fixture is the only one that CREATES the shared
			// table; the other eight MATCH it. Omitting CONTAINS without an
			// anchor would leave every seed unable to attach, all nine branches
			// would come back missing, and the test would fail for a fixture
			// reason rather than prove anything about CONTAINS.
			if branches[omitted].Name == "CONTAINS" {
				if _, err := reader.Run(ctx, `MERGE (t:SqlTable {name: '`+table+`'})`, nil); err != nil {
					t.Fatalf("seed anchor table for the CONTAINS omission: %v", err)
				}
			}
			for i, branch := range branches {
				if i == omitted {
					continue
				}
				if _, err := reader.Run(ctx, branch.Seed, nil); err != nil {
					t.Fatalf("seed branch %q: %v", branch.Name, err)
				}
			}

			rows, err := reader.Run(ctx,
				blastRadiusSqlTableQuery(repositoryAccessFilter{allScopes: true}),
				map[string]any{"target_name": table, "limit": 200})
			if err != nil {
				t.Fatalf("run blast radius: %v", err)
			}

			want := branches[omitted].Name
			missing, got := sqlBlastRadiusMissingBranches(rows, branches, sqlBlastRadiusBitePrefix)
			if len(missing) != 1 || missing[0] != want {
				t.Fatalf("omitted branch %q: missing = %v, want exactly [%s] -- the missing list "+
					"must accumulate from the rows the query returned. A version deriving it from "+
					"the fixture list reports nothing missing even when a shipped UNION branch is "+
					"dead, which is the false green this test exists to stop (#6204)",
					want, missing, want)
			}
			if len(got) != len(branches)-1 {
				t.Errorf("omitted branch %q: %d seeded repositories came back, want %d: %v",
					want, len(got), len(branches)-1, got)
			}
		})
	}
}
