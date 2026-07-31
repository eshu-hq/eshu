// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Prior-generation row-ordering regression tests for loadPriorConfigAddresses
// (issue #5572 follow-up, P2 review finding). Split from
// tfstate_drift_evidence_prior_config_test.go to stay under the CLAUDE.md
// 500-line cap.
package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending is the
// regression for a second reviewer's P2 finding: loadPriorConfigAddresses's
// generationOrder relies on listPriorConfigAddressesQuery's OUTER row order
// being most-recent-generation-first, so first-write-wins keeps the freshest
// generation's confidence signal. The query's CTE bounds which generations
// are selected via `ORDER BY ingested_at DESC LIMIT $3`, but the OUTER
// SELECT previously had its own, DIFFERENT `ORDER BY pg.generation_id ASC,
// fact.fact_id ASC` -- lexicographic on an opaque TEXT primary key
// (schema/data-plane/postgres/002_scope_generations.sql:
// `generation_id TEXT PRIMARY KEY`, no defined chronological relationship;
// this codebase's own scope_generations_scope_latest_lookup_idx exists
// specifically because generation_id is NOT trusted as a recency proxy
// elsewhere). Proven empirically against a real Postgres 18 instance seeded
// with three prior generations whose generation_id ASC order
// (gen-alpha, gen-charlie, gen-omega) differs from their true
// ingested_at DESC order (gen-charlie, gen-alpha, gen-omega): the
// pre-fix query returned rows in generation_id order, not recency order.
// EXPLAIN (ANALYZE, VERBOSE) additionally showed the CTE gets fully inlined
// into a Nested Loop against scope_generations on Postgres 18 (no separate
// CTE Scan node), re-executed once per outer-loop iteration -- confirming
// the CTE's own `ORDER BY ingested_at DESC` bounds membership only, never
// the final row order, with or without inlining. See
// docs/internal/evidence/5572-drift-derived-outcome-module-resolution-confidence.md
// for the full EXPLAIN output from both the broken and fixed query text.
//
// This test asserts the SQL constant text directly because the package's
// fakeExecQueryer test double bypasses real SQL execution entirely -- it
// returns whatever rows a test hands it, in that order, regardless of what
// the query text says. Only a real Postgres planner can prove or disprove a
// row-ordering claim; a text assertion on the shipped query is the durable,
// fast, credential-free guard against a future edit reverting the fix
// without anyone noticing (unit tests using fakeExecQueryer would stay green
// either way).
func TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listPriorConfigAddressesQuery, "ORDER BY pg.ingested_at DESC") {
		t.Fatalf("listPriorConfigAddressesQuery must ORDER BY pg.ingested_at DESC on the OUTER SELECT (not just the CTE), so generationOrder is genuinely most-recent-first; got:\n%s", listPriorConfigAddressesQuery)
	}
	if !strings.Contains(listPriorConfigAddressesQuery, "pg.ingested_at") {
		t.Fatalf("the prior_generations CTE must project ingested_at so the outer ORDER BY can reference it; got:\n%s", listPriorConfigAddressesQuery)
	}
}

// TestCollectPriorConfigAddressesFirstWriteWinsDependsOnCallOrder is a
// direct, fakeExecQueryer-free proof of WHY
// TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending's SQL-level
// guarantee matters: collectPriorConfigAddresses's first-write-wins map
// update means whichever generation's entries are processed FIRST
// permanently decides the address's confidence, and this function has no
// way to know or check whether the caller handed it generations in
// chronological order. loadPriorConfigAddresses's generationOrder slice is
// built strictly from the order rows arrive from the DB -- correctness
// depends entirely on the query, not on anything in this file.
func TestCollectPriorConfigAddressesFirstWriteWinsDependsOnCallOrder(t *testing.T) {
	t.Parallel()

	cleanEntry := map[string]any{
		"path": "main.tf", "resource_type": "aws_instance", "resource_name": "web",
	}
	flaggedEntry := map[string]any{
		"path": "terraform-aws-modules/vpc/aws/main.tf", "resource_type": "aws_instance", "resource_name": "web",
	}
	flaggedConfidence := moduleResolutionConfidenceMap{"terraform-aws-modules/vpc/aws": unresolvedReasonExternalRegistry}

	t.Run("clean generation processed first keeps the address unflagged", func(t *testing.T) {
		t.Parallel()
		out := map[string]string{}
		collectPriorConfigAddresses(cleanEntry, modulePrefixMap{}, moduleResolutionConfidenceMap{}, out)
		collectPriorConfigAddresses(flaggedEntry, modulePrefixMap{}, flaggedConfidence, out)
		if got := out["aws_instance.web"]; got != "" {
			t.Fatalf(`out["aws_instance.web"] = %q, want "" (whichever generation is processed FIRST wins -- here, the clean one)`, got)
		}
	})

	t.Run("flagged generation processed first stamps the reason onto the address", func(t *testing.T) {
		t.Parallel()
		out := map[string]string{}
		collectPriorConfigAddresses(flaggedEntry, modulePrefixMap{}, flaggedConfidence, out)
		collectPriorConfigAddresses(cleanEntry, modulePrefixMap{}, moduleResolutionConfidenceMap{}, out)
		if got, want := out["aws_instance.web"], unresolvedReasonExternalRegistry; got != want {
			t.Fatalf(`out["aws_instance.web"] = %q, want %q -- same two entries, reversed call order, opposite result. This is exactly why loadPriorConfigAddresses's row order must be chronological (see TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending), not left to an unordered outer SELECT`, got, want)
		}
	})
}

// TestPostgresDriftEvidenceLoaderPrefersMostRecentPriorGenerationConfidenceOnConflict
// is the end-to-end regression for the P2 finding: TWO prior generations
// both declare "aws_instance.web", with CONFLICTING confidence -- gen-b (the
// more recently ingested one) has its module resolution fail
// (external_registry); gen-a (older) resolves cleanly. The fixture feeds
// listPriorConfigAddressesQuery's row response in the fixed query's
// guaranteed order (gen-b's row first, matching ingested_at DESC), proving
// that GIVEN correctly-ordered input, loadPriorConfigAddresses's
// first-write-wins threading picks the freshest generation's confidence
// end to end onto the resulting removed_from_config candidate -- not merely
// "some" confidence, but specifically the one a correctly-ordered query
// would deliver first. Reversing the two response rows in this fixture
// would flip the assertion's expected value to "", which is the same
// order-sensitivity TestCollectPriorConfigAddressesFirstWriteWinsDependsOnCallOrder
// proves directly; this test additionally proves the full loader wiring
// (buildModulePrefixMap per generation, mergeDriftRows's promotion) carries
// that value through to the durable AddressedRow. Before this test, no
// regression exercised more than one prior generation for the same address,
// which is why the SQL ordering bug went unnoticed.
func TestPostgresDriftEvidenceLoaderPrefersMostRecentPriorGenerationConfidenceOnConflict(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a2",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules CURRENT gen: empty.
			{rows: [][]any{}},
			// 2. config-side CURRENT gen: empty (resource deleted from config).
			{rows: [][]any{}},
			// 3. current snapshot: serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// 4. current state-resource: real resource at the root address --
			//    matches whichever prior generation's fallback address, by
			//    construction of this fixture (both prior generations below
			//    project to "aws_instance.web").
			{rows: [][]any{fixtureStateResourceRow(
				"aws_instance.web",
				fixtureStatePayload("aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 5. prior state snapshot: same lineage, serial=4.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// 6. prior state-resource: same address.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_instance.web",
				fixtureStatePayload("aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 7. prior-config addresses walk: TWO generations in ONE result
			//    set, fed in the FIXED query's guaranteed order --
			//    ingested_at DESC, so the more recently ingested "gen-b"
			//    arrives BEFORE the older "gen-a".
			{rows: [][]any{
				{
					"gen-b",
					fixturePriorConfigAddressesArray(fixtureConfigParserRowAtPath(
						"aws_instance", "web", "terraform-aws-modules/vpc/aws/main.tf",
					)),
				},
				{
					"gen-a",
					fixturePriorConfigAddressesArray(fixtureConfigParserRowAtPath(
						"aws_instance", "web", "main.tf",
					)),
				},
			}},
			// 8. terraform_modules for gen-b (processed first): the
			//    registry-shorthand misclassification that flags this
			//    generation's projection of "aws_instance.web" low-confidence.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc", "terraform-aws-modules/vpc/aws", "main.tf"),
			)}}},
			// 9. terraform_modules for gen-a (processed second): no module
			//    calls -- clean resolution. Must NOT override gen-b's
			//    already-recorded reason (first-write-wins).
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db, PriorConfigDepth: 10}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if rows[0].State == nil {
		t.Fatalf("row.State = nil, want non-nil")
	}
	if !rows[0].State.PreviouslyDeclaredInConfig {
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = false, want true")
	}
	if got, want := rows[0].State.ModuleResolutionReason, "external_registry"; got != want {
		t.Fatalf("row.State.ModuleResolutionReason = %q, want %q (the more recently ingested generation's confidence must win over the older, conflicting one)", got, want)
	}
}
