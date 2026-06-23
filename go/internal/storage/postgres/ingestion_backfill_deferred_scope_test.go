package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

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
