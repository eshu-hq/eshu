package postgres

import (
	"context"
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
// the fix: the deferred pass issues the self-exclusion query variant (which
// carries a third parameter — the repo_id values to exclude as self-match
// candidates), not the per-commit scoped query that has no self-exclusion.
func TestBackfillDeferredPassExcludesSelfRepoIDMatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 23, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-infra", "scope-infra", "gen-infra"},
		{"repo-app", "scope-app", "gen-app"},
	}
	// Two catalog entries: repo-infra (aliases: ["repo-infra","infra-repo"]) and
	// repo-app (aliases: ["repo-app","app-repo"]). The payload for each fact
	// carries its OWN repo_id. Without the self-exclusion fix, both facts would
	// self-match and the load would be corpus-wide. With the fix, only the fact
	// that references the OTHER repo's name/slug alias is loaded.
	inner := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// catalog: two repos, each with repo_id as first alias
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// Deferred scoped fact query result (self-exclusion variant): only the
			// infra fact that references "app-repo" in content is returned; the
			// fact that would have self-matched "repo-infra" in its own payload is
			// excluded by the self-exclusion predicate at the SQL layer.
			{
				rows: [][]any{
					contentFactRow(
						"fact-cross",
						"scope-infra",
						"gen-infra",
						"content",
						`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`,
					),
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

	// The deferred pass must use the self-exclusion query variant, which carries
	// THREE parameters ($1 non-repo_id anchors, $2 repo_id anchors, $3 self
	// repo_id values to exclude), NOT the per-commit scoped query (one parameter).
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
// with three arguments: non-repo_id LIKE terms, repo_id LIKE terms, and the raw
// repo_id values for the self-exclusion NOT LIKE predicate.
func assertDeferredSelfExclusionArgs(t *testing.T, args []any) {
	t.Helper()
	if len(args) != 3 {
		t.Fatalf("deferred fact query args count = %d, want 3 (non-repo_id anchors, repo_id anchors, repo_id exclusion values)", len(args))
	}
	nonRepoIDTerms, ok := args[0].(pq.StringArray)
	if !ok {
		t.Fatalf("deferred query arg[0] type = %T, want pq.StringArray", args[0])
	}
	repoIDTerms, ok := args[1].(pq.StringArray)
	if !ok {
		t.Fatalf("deferred query arg[1] type = %T, want pq.StringArray", args[1])
	}
	repoIDValues, ok := args[2].(pq.StringArray)
	if !ok {
		t.Fatalf("deferred query arg[2] type = %T, want pq.StringArray", args[2])
	}
	// non-repo_id terms must be LIKE-wrapped
	for _, term := range nonRepoIDTerms {
		if !strings.HasPrefix(term, "%") || !strings.HasSuffix(term, "%") {
			t.Fatalf("non-repo_id anchor term %q is not a wrapped LIKE term", term)
		}
	}
	// repo_id terms must be LIKE-wrapped
	for _, term := range repoIDTerms {
		if !strings.HasPrefix(term, "%") || !strings.HasSuffix(term, "%") {
			t.Fatalf("repo_id anchor term %q is not a wrapped LIKE term", term)
		}
	}
	// repo_id values must be plain (not LIKE-wrapped) — used in != ALL($3)
	for _, val := range repoIDValues {
		if strings.HasPrefix(val, "%") || strings.HasSuffix(val, "%") {
			t.Fatalf("repo_id exclusion value %q must not be LIKE-wrapped", val)
		}
	}
	// There must be at least one repo_id term and one exclusion value
	if len(repoIDTerms) == 0 {
		t.Fatal("deferred query repo_id anchor terms is empty; self-exclusion predicate is inoperative")
	}
	if len(repoIDValues) == 0 {
		t.Fatal("deferred query repo_id exclusion values is empty; self-exclusion predicate is inoperative")
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
	inner := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// catalog
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// anchor-scoped relationship facts (issue #3569)
			{
				rows: [][]any{
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

	usedScopedQuery := false
	usedFullCorpusQuery := false
	for _, q := range inner.queries {
		if q.query == listOnboardedRepoScopedRelationshipFactRecordsQuery {
			usedScopedQuery = true
			assertScopedAnchorArg(t, q.args)
		}
		if q.query == listLatestRelationshipFactRecordsQuery {
			usedFullCorpusQuery = true
		}
	}
	if usedFullCorpusQuery {
		t.Fatal("deferred backfill issued the unbounded full-corpus fact query; it must use the anchor-scoped query")
	}
	if !usedScopedQuery {
		t.Fatal("deferred backfill did not issue the anchor-scoped fact query")
	}
}

// assertScopedAnchorArg confirms the scoped fact query was parameterised with a
// non-empty %...% LIKE term array, proving the query is bounded by the catalog
// anchor surface rather than scanning every fact.
func assertScopedAnchorArg(t *testing.T, args []any) {
	t.Helper()
	if len(args) != 1 {
		t.Fatalf("scoped fact query args = %d, want 1 anchor array", len(args))
	}
	terms, ok := args[0].(pq.StringArray)
	if !ok {
		t.Fatalf("scoped fact query arg type = %T, want pq.StringArray", args[0])
	}
	if len(terms) == 0 {
		t.Fatal("scoped fact query anchor array is empty; the load is not bounded")
	}
	for _, term := range terms {
		if !strings.HasPrefix(term, "%") || !strings.HasSuffix(term, "%") {
			t.Fatalf("scoped anchor term %q is not a wrapped LIKE substring term", term)
		}
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
			q.query == listLatestRelationshipFactRecordsQuery {
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
