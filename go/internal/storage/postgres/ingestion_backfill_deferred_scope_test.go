// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// TestBackfillDeferredPassExcludesSelfRepoIDMatch is the issue #3659 regression
// gate: when a fact's payload carries its own repo_id (e.g.
// `{"repo_id":"repo-infra",...}`), the deferred backfill's anchor-scoped SQL
// query must NOT load that fact solely because the catalog anchor set includes
// "repo-infra" (the repo_id of that repo's own catalog entry). The fact may
// only be loaded if its payload ALSO contains a non-repo_id anchor (a name/slug
// token or an ArgoCD marker) or the repo_id of a DIFFERENT catalog entry.
//
// Without the fix the deferred query is corpus-wide despite the LIKE ANY
// predicate, because every fact's "repo_id" payload field self-matches the
// repo_id anchor derived from the same repo's catalog entry. The test proves
// the fix: the deferred pass issues the self-exclusion query variant (with raw
// repo_id values for exact self-exclusion), not the per-commit scoped query that
// has no self-exclusion.
func TestBackfillDeferredPassExcludesSelfRepoIDMatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 23, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-infra", "scope-infra", "gen-infra"},
		{"repo-app", "scope-app", "gen-app"},
	}
	// Fact-load partitioning is keyed on the (scope_id, generation_id) pair (issue
	// #3710), so its snapshot has two columns, not the three of the repository-
	// generation write snapshots.
	scopeGenPartitions := [][]any{
		{"scope-infra", "gen-infra"},
		{"scope-app", "gen-app"},
	}
	// Two catalog entries: repo-infra (aliases: ["repo-infra","infra-repo"]) and
	// repo-app (aliases: ["repo-app","app-repo"]). The payload for each fact
	// carries its OWN repo_id. Without the self-exclusion fix, both facts would
	// self-match and the load would be corpus-wide. With the fix, only the fact
	// that references the OTHER repo's name/slug alias is loaded.
	inner := &fakeExecQueryer{
		// Deferred scoped fact query result (self-exclusion variant), routed by
		// scope (issue #3710): only the infra fact that references "app-repo" in
		// content is returned for scope-infra; the fact that would have self-matched
		// "repo-infra" in its own payload is excluded by the SQL self-exclusion arm.
		deferredFactsByScope: map[string][][]any{
			"scope-infra": {
				contentFactRow(
					"fact-cross",
					"scope-infra",
					"gen-infra",
					"content",
					`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`,
				),
			},
		},
		queryResponses: []queueFakeRows{
			// catalog: two repos, each with repo_id as first alias
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// scope-generation partition snapshot (fact-load partitioning, #3710)
			{rows: scopeGenPartitions},
			// active repository generations snapshot (write phase)
			{rows: activeGens},
			// batch transaction re-load of active generations under the lock
			{rows: activeGens},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	// The deferred pass must use the self-exclusion query variant, which carries
	// four parameters ($1 non-repo_id anchors, $2 self-excluded repo_id values, $3
	// scope_id, $4 generation_id), NOT the per-commit scoped query (one parameter).
	usedDeferredQuery := false
	usedPerCommitQuery := false
	for _, q := range inner.queries {
		if q.query == listDeferredScopedRelationshipFactRecordsQuery {
			usedDeferredQuery = true
			assertDeferredSelfExclusionArgs(t, q.args)
		}
		if q.query == listOnboardedRepoScopedRelationshipFactRecordsQuery {
			usedPerCommitQuery = true
		}
	}
	if usedPerCommitQuery && !usedDeferredQuery {
		t.Fatal("deferred backfill issued per-commit query without self-exclusion; must use the deferred self-exclusion query variant")
	}
	if !usedDeferredQuery {
		t.Fatal("deferred backfill did not issue the deferred self-exclusion fact query")
	}
}

// assertDeferredSelfExclusionArgs confirms the deferred query was parameterised
// with seven arguments (issue #3624 payload-hoist rewrite): $1 non-repo_id LIKE
// terms, $2 raw lowercase repo_id values for exact self-exclusion, $3 scope_id
// partition, $4 generation_id partition, $5 the nullable $6-excluded regex
// alternation (buildDeferredRepoIDRegex), and $6 the scope_id-derived own-repo_id
// performance hint (deferredScopedFactOwnRepoIDFromScope), and $7 repo_id
// reference token keys for the side-table fast path. The query uses the
// raw repo_id values for exact self-exclusion before literal substring matching,
// so repo_id args must not be %-wrapped LIKE terms.
func assertDeferredSelfExclusionArgs(t *testing.T, args []any) {
	t.Helper()
	if len(args) != 7 {
		t.Fatalf("deferred fact query args count = %d, want 7 (non-repo_id anchors, repo_id values, scope_id, generation_id, own-excluded regex, own repo_id, repo_id reference keys)", len(args))
	}
	nonRepoIDTerms, ok := args[0].(pq.StringArray)
	if !ok {
		t.Fatalf("deferred query arg[0] type = %T, want pq.StringArray", args[0])
	}
	repoIDTerms, ok := args[1].(pq.StringArray)
	if !ok {
		t.Fatalf("deferred query arg[1] type = %T, want pq.StringArray", args[1])
	}
	if _, ok := args[2].(string); !ok {
		t.Fatalf("deferred query arg[2] (scope_id) type = %T, want string", args[2])
	}
	if _, ok := args[3].(string); !ok {
		t.Fatalf("deferred query arg[3] (generation_id) type = %T, want string", args[3])
	}
	if _, ok := args[4].(sql.NullString); !ok {
		t.Fatalf("deferred query arg[4] ($5 own-excluded regex) type = %T, want sql.NullString", args[4])
	}
	if _, ok := args[5].(string); !ok {
		t.Fatalf("deferred query arg[5] ($6 own repo_id hint) type = %T, want string", args[5])
	}
	repoIDKeys, ok := args[6].(pq.StringArray)
	if !ok {
		t.Fatalf("deferred query arg[6] ($7 repo_id reference keys) type = %T, want pq.StringArray", args[6])
	}
	if len(repoIDKeys) != len(repoIDTerms) {
		t.Fatalf("deferred query arg[6] keys = %d, want one key per repo_id term (%d)", len(repoIDKeys), len(repoIDTerms))
	}
	// non-repo_id terms must be LIKE-wrapped
	for _, term := range nonRepoIDTerms {
		if !strings.HasPrefix(term, "%") || !strings.HasSuffix(term, "%") {
			t.Fatalf("non-repo_id anchor term %q is not a wrapped LIKE term", term)
		}
	}
	// repo_id values must be raw literals, not LIKE-wrapped terms. Wrapping here
	// would defeat the exact self-exclusion comparison in SQL.
	for _, term := range repoIDTerms {
		if strings.HasPrefix(term, "%") || strings.HasSuffix(term, "%") {
			t.Fatalf("repo_id-value anchor term %q is LIKE-wrapped; want raw lowercase repo_id", term)
		}
		if term != strings.ToLower(term) {
			t.Fatalf("repo_id-value anchor term %q is not lowercase", term)
		}
	}
	// There must be at least one repo_id-value term so the self-exclusion arm is live.
	if len(repoIDTerms) == 0 {
		t.Fatal("deferred query repo_id-value anchor terms is empty; the cross-repo repo_id arm is inoperative")
	}
}

// TestBackfillDeferredFactLoadPartitionsOnScopeGenerationNotRepository is the
// non-DB CI-regression guard for the issue #3710 P0 fix: the deferred backfill's
// fact-LOAD phase MUST source its partitions from activeScopeGenerationPartitionsQuery
// (loadActiveScopeGenerationPartitions), NOT from activeRepositoryGenerationsQuery
// (loadActiveRepositoryGenerations).
//
// Why this guard exists. The P0 fix is otherwise only proven by
// ingestion_backfill_partition_integration_test.go, which is gated on
// ESHU_DEFERRED_PARTITION_PROOF_DSN and SKIPS in normal CI. The non-DB fakes
// return canned rows in FIFO order without asserting WHICH query string fetched
// them, so a future revert of loadActiveScopeGenerationPartitions back to
// loadActiveRepositoryGenerations would silently pass CI and re-drop every
// gcp_cloud_relationship fact (those facts live in cloud scopes that carry no
// repository fact, so the repository-generation source never partitions them).
//
// The query strings are distinct const identities (both share latestGenerationCTE
// but differ in their SELECT), so this records and compares the exact SQL the run
// issued. It asserts:
//
//	(a) activeScopeGenerationPartitionsQuery IS issued before the first per-scope
//	    fact query (the fact-load partition source), and
//	(b) activeRepositoryGenerationsQuery is NOT issued during the fact-load phase
//	    (everything before the first per-scope fact query). The write phase still
//	    legitimately issues activeRepositoryGenerationsQuery AFTER the fact load, so
//	    the assertion is bounded to the pre-fact-load prefix by order, not by a
//	    blanket "never issued" check.
//
// Swap loadActiveScopeGenerationPartitions -> loadActiveRepositoryGenerations in
// loadDeferredAnchorScopedRelationshipFacts and this test fails on (a): the
// scope-generation query is never issued, and the repository-generation query
// appears in the fact-load prefix instead.
func TestBackfillDeferredFactLoadPartitionsOnScopeGenerationNotRepository(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 23, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-infra", "scope-infra", "gen-infra"},
		{"repo-app", "scope-app", "gen-app"},
	}
	scopeGenPartitions := [][]any{
		{"scope-infra", "gen-infra"},
		{"scope-app", "gen-app"},
	}
	inner := &fakeExecQueryer{
		deferredFactsByScope: map[string][][]any{
			"scope-infra": {
				contentFactRow(
					"fact-cross",
					"scope-infra",
					"gen-infra",
					"content",
					`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`,
				),
			},
		},
		queryResponses: []queueFakeRows{
			// catalog: two repos, each with repo_id as first alias
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// scope-generation partition snapshot (fact-load partitioning, #3710)
			{rows: scopeGenPartitions},
			// active repository generations snapshot (write phase)
			{rows: activeGens},
			// batch transaction re-load of active generations under the lock
			{rows: activeGens},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	// Locate the first per-scope deferred fact query; everything before it is the
	// fact-load partition-source phase. The write phase (which legitimately issues
	// activeRepositoryGenerationsQuery) only runs after this index.
	firstDeferredFactIdx := -1
	for i, q := range inner.queries {
		if q.query == listDeferredScopedRelationshipFactRecordsQuery {
			firstDeferredFactIdx = i
			break
		}
	}
	if firstDeferredFactIdx < 0 {
		t.Fatal("deferred backfill never issued the per-scope fact query; cannot locate the fact-load phase")
	}

	sawScopeGenerationSource := false
	for _, q := range inner.queries[:firstDeferredFactIdx] {
		if q.query == activeScopeGenerationPartitionsQuery {
			sawScopeGenerationSource = true
		}
		if q.query == activeRepositoryGenerationsQuery {
			t.Fatal("deferred backfill fact-load partitioned on activeRepositoryGenerationsQuery; " +
				"it MUST partition on activeScopeGenerationPartitionsQuery so gcp cloud-scope facts and " +
				"collapsing scopes are not dropped (issue #3710 P0)")
		}
	}
	if !sawScopeGenerationSource {
		t.Fatal("deferred backfill fact-load did not issue activeScopeGenerationPartitionsQuery as its " +
			"partition source; a revert to loadActiveRepositoryGenerations would silently re-drop gcp facts")
	}
}

// TestBackfillAllRelationshipEvidenceUsesScopedFactQuery is the issue #3569
// scope-bounding gate: the corpus-wide deferred backfill MUST load source facts
// through the content-anchored scoped query (parameterised LIKE-ANY predicate),
// never the unbounded full-corpus listLatestRelationshipFactRecordsQuery. The
// scoped query carries a $1 anchor parameter; the full-corpus query has none.
func TestBackfillAllRelationshipEvidenceUsesScopedFactQuery(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-infra", "scope-infra", "gen-infra"},
		{"repo-app", "scope-app", "gen-app"},
	}
	scopeGenPartitions := [][]any{
		{"scope-infra", "gen-infra"},
		{"scope-app", "gen-app"},
	}
	inner := &fakeExecQueryer{
		// anchor-scoped relationship facts (issue #3569), per-scope routed (#3710)
		deferredFactsByScope: map[string][][]any{
			"scope-infra": {
				{
					"fact-1",
					"scope-infra",
					"gen-infra",
					"content",
					"content:1",
					"content.v1",
					"git",
					int64(0),
					"unknown",
					"git",
					"source-fact-1",
					"",
					"",
					now,
					false,
					[]byte(`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`),
				},
			},
		},
		queryResponses: []queueFakeRows{
			// catalog
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// scope-generation partition snapshot (fact-load partitioning, #3710)
			{rows: scopeGenPartitions},
			// active repository generations snapshot (write phase)
			{rows: activeGens},
			// batch transaction re-load of active generations under the lock
			{rows: activeGens},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	usedDeferredQuery := false
	usedPerCommitQuery := false
	usedFullCorpusQuery := false
	for _, q := range inner.queries {
		if q.query == listDeferredScopedRelationshipFactRecordsQuery {
			usedDeferredQuery = true
			assertDeferredSelfExclusionArgs(t, q.args)
		}
		if q.query == listOnboardedRepoScopedRelationshipFactRecordsQuery {
			usedPerCommitQuery = true
		}
		if q.query == listLatestRelationshipFactRecordsQuery {
			usedFullCorpusQuery = true
		}
	}
	if usedFullCorpusQuery {
		t.Fatal("deferred backfill issued the unbounded full-corpus fact query; it must use the deferred self-exclusion query")
	}
	if usedPerCommitQuery {
		t.Fatal("deferred backfill issued the per-commit scoped query (no self-exclusion); it must use the deferred self-exclusion query")
	}
	if !usedDeferredQuery {
		t.Fatal("deferred backfill did not issue the deferred self-exclusion fact query")
	}
}

// TestBackfillAllRelationshipEvidenceShortCircuitsWithoutAnchors pins that when
// the catalog yields no usable anchors (no repository has an alias token), the
// deferred backfill never issues any source-fact query at all: with no anchor no
// content/file/gcp fact can resolve a catalog target, so the corpus-wide scan is
// pure waste. Readiness is still published for the active generations.
func TestBackfillAllRelationshipEvidenceShortCircuitsWithoutAnchors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-app", "scope-app", "gen-app"},
	}
	inner := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// catalog rows with no decodable repo identity -> no anchors
			{
				rows: [][]any{
					{[]byte(`{"unrelated":"value"}`)},
				},
			},
			// active repository generations snapshot
			{rows: activeGens},
			// batch transaction re-load of active generations under the lock
			{rows: activeGens},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	for _, q := range inner.queries {
		if q.query == listOnboardedRepoScopedRelationshipFactRecordsQuery ||
			q.query == listLatestRelationshipFactRecordsQuery ||
			q.query == listDeferredScopedRelationshipFactRecordsQuery {
			t.Fatalf("deferred backfill issued a fact query with no usable anchors: %s", q.query)
		}
	}
	foundPhasePublish := false
	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO graph_projection_phase_state") {
			foundPhasePublish = true
			break
		}
	}
	if !foundPhasePublish {
		t.Fatal("expected backward evidence readiness publish even with no anchors")
	}
}
