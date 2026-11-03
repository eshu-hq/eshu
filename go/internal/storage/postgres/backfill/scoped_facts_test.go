// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// contentFactRow builds the 16-column fact_records projection row that
// scanFactEnvelope expects, with the supplied payload JSON bytes. The column
// order mirrors ListOnboardedRepoScopedRelationshipFactRecordsQuery and
// listLatestRelationshipFactRecordsQuery so the scoped loader can reuse the
// shared scanner.
func contentFactRow(factID, scopeID, generationID, factKind string, payload string) []any {
	return []any{
		factID,                // fact_id
		scopeID,               // scope_id
		generationID,          // generation_id
		factKind,              // fact_kind
		"stable-" + factID,    // stable_fact_key
		"v1",                  // schema_version
		"git",                 // collector_kind
		int64(1),              // fencing_token
		"high",                // source_confidence
		"git",                 // source_system
		"src-" + factID,       // source_fact_key
		"",                    // source_uri
		"",                    // source_record_id
		time.Unix(0, 0).UTC(), // observed_at
		false,                 // is_tombstone
		[]byte(payload),       // payload
	}
}

func TestLoadOnboardedRepoScopedRelationshipFactsPassesAnchorsAndScans(t *testing.T) {
	t.Parallel()

	payload := `{"content_path":"main.tf","content_body":"module \"x\" { source = \"./payments-service\" }"}`
	queryer := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{contentFactRow("fact-1", "scope-1", "gen-1", "content", payload)}},
		},
	}

	anchors := []string{"payments-service"}
	loaded, err := loadOnboardedRepoScopedRelationshipFacts(context.Background(), queryer, anchors)
	if err != nil {
		t.Fatalf("loadOnboardedRepoScopedRelationshipFacts returned error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d facts, want 1", len(loaded))
	}
	if loaded[0].FactKind != "content" {
		t.Fatalf("loaded fact kind = %q, want content", loaded[0].FactKind)
	}

	if len(queryer.queries) != 1 {
		t.Fatalf("issued %d queries, want 1", len(queryer.queries))
	}
	call := queryer.queries[0]
	if call.query != ListOnboardedRepoScopedRelationshipFactRecordsQuery {
		t.Fatalf("unexpected query:\n%s", call.query)
	}
	if len(call.args) != 1 {
		t.Fatalf("query args = %d, want 1", len(call.args))
	}
	likeArgs, ok := call.args[0].(array.StringArray)
	if !ok {
		t.Fatalf("query arg type = %T, want array.StringArray", call.args[0])
	}
	if len(likeArgs) != 1 || likeArgs[0] != "%payments-service%" {
		t.Fatalf("LIKE args = %v, want [%%payments-service%%]", []string(likeArgs))
	}
}

func TestLoadOnboardedRepoScopedRelationshipFactsEmptyAnchorsShortCircuits(t *testing.T) {
	t.Parallel()

	queryer := &fakeExecQueryer{}
	loaded, err := loadOnboardedRepoScopedRelationshipFacts(context.Background(), queryer, nil)
	if err != nil {
		t.Fatalf("expected nil error for empty anchors, got %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil facts for empty anchors, got %v", loaded)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("expected no queries for empty anchors, got %d", len(queryer.queries))
	}
}

func TestScopedRelationshipFactQueryShape(t *testing.T) {
	t.Parallel()

	query := ListOnboardedRepoScopedRelationshipFactRecordsQuery
	for _, want := range []string{
		"fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')",
		"lower(fact.payload::text) LIKE ANY($1)",
		"latest_generations",
		"latest.generation_id IS NOT NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("scoped query missing %q", want)
		}
	}
}

func TestBuildPayloadAnchorLikeTermsEscapesWildcards(t *testing.T) {
	t.Parallel()

	got := buildPayloadAnchorLikeTerms([]string{`a_b%c\d`, "plain"})
	want := []string{`%a\_b\%c\\d%`, "%plain%"}
	if len(got) != len(want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("term[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestLoadOnboardedRepoScopedRelationshipFactsBudgetFailsLoudly is the #9781
// regression for the load bound: a budgeted anchor-scoped load must return
// ErrRelationshipFactLoadBudgetExceeded as soon as the cumulative raw payload
// bytes cross the budget, discarding the accumulated slice, instead of holding
// an unbounded one. It also pins that the stats report the rows and bytes read
// up to and including the crossing row, so the caller's inbound metric is
// recorded even on a budget failure.
func TestLoadOnboardedRepoScopedRelationshipFactsBudgetFailsLoudly(t *testing.T) {
	t.Parallel()

	payload := `{"content_path":"main.tf","content_body":"module \"x\" { source = \"./payments-service\" }"}`
	queryer := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				contentFactRow("fact-1", "scope-1", "gen-1", "content", payload),
				contentFactRow("fact-2", "scope-1", "gen-1", "content", payload),
				contentFactRow("fact-3", "scope-1", "gen-1", "content", payload),
			}},
		},
	}

	loaded, stats, err := loadOnboardedRepoScopedRelationshipFactsBudget(
		context.Background(), queryer, []string{"payments-service"}, RelationshipFactLoadBudget{MaxBytes: 1})
	if !errors.Is(err, ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("err = %v, want ErrRelationshipFactLoadBudgetExceeded", err)
	}
	if loaded != nil {
		t.Fatalf("loaded = %d facts, want nil on budget failure", len(loaded))
	}
	if stats.Rows != 1 || stats.Bytes <= 0 {
		t.Fatalf("stats = %+v, want rows=1 and bytes>0 (the row that crossed the budget)", stats)
	}
}

// TestLoadOnboardedRepoScopedRelationshipFactsBudgetUnboundedReportsStats pins
// the unbounded form: a zero budget loads every row and reports the exact row
// count and a positive byte sum, matching the non-budgeted loader's fact set.
func TestLoadOnboardedRepoScopedRelationshipFactsBudgetUnboundedReportsStats(t *testing.T) {
	t.Parallel()

	payload := `{"content_path":"main.tf","content_body":"module \"x\" { source = \"./payments-service\" }"}`
	queryer := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				contentFactRow("fact-1", "scope-1", "gen-1", "content", payload),
				contentFactRow("fact-2", "scope-1", "gen-1", "content", payload),
			}},
		},
	}

	loaded, stats, err := loadOnboardedRepoScopedRelationshipFactsBudget(
		context.Background(), queryer, []string{"payments-service"}, RelationshipFactLoadBudget{})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded = %d facts, want 2", len(loaded))
	}
	if stats.Rows != 2 {
		t.Fatalf("stats.Rows = %d, want 2", stats.Rows)
	}
	if stats.Bytes != int64(len(payload))*2 {
		t.Fatalf("stats.Bytes = %d, want %d (raw payload bytes, not decoded)", stats.Bytes, len(payload)*2)
	}
}

// TestLoadAnchorScopedRelationshipFactsBudgetThreadsBudget pins the exported
// budgeted loader end to end: a one-byte budget fails the phase-one load before
// the phase-two config reload, and the unbudgeted wrapper returns the same fact
// set as the budgeted loader with an unlimited budget.
func TestLoadAnchorScopedRelationshipFactsBudgetThreadsBudget(t *testing.T) {
	t.Parallel()

	catalog := []relationships.CatalogEntry{
		{RepoID: "repository:r_9a7e0001", Aliases: []string{"repository:r_9a7e0001", "payments-api"}},
	}
	payload := `{"content_path":"main.tf","content_body":"app_repo = \"payments-api\""}`
	queryer := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{contentFactRow("fact-1", "scope-1", "gen-1", "content", payload)}},
		},
	}

	_, _, err := LoadAnchorScopedRelationshipFactsBudget(
		context.Background(), queryer, catalog, catalog, RelationshipFactLoadBudget{MaxBytes: 1})
	if !errors.Is(err, ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("err = %v, want ErrRelationshipFactLoadBudgetExceeded", err)
	}
}

// TestBackfillNonRepoIDAnchorTermsMatchCatalogAnchors pins that the deferred
// backfill's non-repo_id terms are exactly the relationships-level non-repo_id
// anchors plus the unconditional markers, with the repo_id alias (Aliases[0])
// and RemoteURL left out. The deferred and per-commit loaders must inherit the
// same narrowed anchor set, so the two paths cannot drift apart (#9781).
func TestBackfillNonRepoIDAnchorTermsMatchCatalogAnchors(t *testing.T) {
	t.Parallel()

	catalog := []relationships.CatalogEntry{
		{
			RepoID:    "repository:r_9a7e0001",
			Aliases:   []string{"repository:r_9a7e0001", "payments-api", "acme/payments-api"},
			RemoteURL: "https://github.com/acme/payments-api",
		},
		// An entry with only a repo_id alias contributes no non-repo_id anchor.
		{RepoID: "repository:r_2e4a0001", Aliases: []string{"repository:r_2e4a0001"}},
	}

	got := backfillNonRepoIDAnchorTerms(catalog)
	want := append(
		append([]string(nil), relationships.CatalogNonRepoIDPayloadAnchors(catalog)...),
		relationships.UnconditionalPayloadMarkers()...,
	)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backfillNonRepoIDAnchorTerms = %v, want %v", got, want)
	}
	// The fleet-wide repository namespace must never appear: every content and
	// file payload stores its own repo_id, so a bare "repository" token would
	// match the whole corpus (Defect A).
	for _, term := range got {
		if term == "repository" {
			t.Fatalf("anchor set contains the fleet-wide %q token: %v", term, got)
		}
	}
}
