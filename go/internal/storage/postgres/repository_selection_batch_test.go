// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Batch annotation tests for PageSelectionStore (#8454). The scripted fake
// serves the four batch statements by query shape; the QA-shape test at the
// bottom extends the 802-scope QA corpus behind #7625 with probe verdicts
// and proves the page aggregates annotate exactly the unlisted scopes.
package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

// batchNow anchors the fixture to the test-run clock (#8658): the store
// reads with the real wall clock, so a fixed fixture timestamp goes stale
// once its 48h liveness window passes and every scope reads unknown with no
// follow-up queries. All fixture offsets stay relative, so liveness,
// confirmation, and rule (d) invariants hold at any run time.
var batchNow = time.Now().UTC().Truncate(time.Second)

// batchObsRow is one liveScopeObservationsBatchQuery row.
func batchObsRow(scope, selector, state string, cycles int, classification, successor string) []any {
	since := batchNow.Add(-48 * time.Hour)
	classifiedAt := sql.NullTime{}
	classificationValue := sql.NullString{}
	successorValue := sql.NullString{}
	if classification != "" {
		classificationValue = sql.NullString{String: classification, Valid: true}
		successorValue = sql.NullString{String: successor, Valid: classification != "still_present" && classification != "not_found_or_inaccessible"}
		if successor == "" {
			successorValue = sql.NullString{}
		}
		classifiedAt = sql.NullTime{Time: batchNow.Add(-time.Hour), Valid: true}
	}
	return []any{
		scope, selector, state,
		sql.NullTime{},
		since, cycles, batchNow, 172800,
		classificationValue, successorValue, classifiedAt,
	}
}

// batchScript serves the four batch statements from fixture maps.
type batchScript struct {
	obs  map[string][][]any
	gens map[string]time.Time
	slug map[string]string
}

func (s batchScript) route(query string, args []any) (*fake.Rows, bool) {
	switch {
	case strings.Contains(query, "MAX(observed_at)"):
		var data [][]any
		for _, id := range args[0].([]string) {
			if at, ok := s.gens[id]; ok {
				data = append(data, []any{id, at})
			}
		}
		return &fake.Rows{Data: data}, true
	case strings.Contains(query, "payload->>'repo_slug'"):
		var data [][]any
		for _, slug := range args[0].([]string) {
			for stored, scope := range s.slug {
				if strings.EqualFold(stored, slug) {
					data = append(data, []any{scope, stored})
				}
			}
		}
		return &fake.Rows{Data: data}, true
	case strings.Contains(query, "scope_id = ANY"):
		var data [][]any
		for _, id := range args[0].([]string) {
			data = append(data, s.obs[id]...)
		}
		return &fake.Rows{Data: data}, true
	default:
		return nil, false
	}
}

func batchQueries(database *fake.ExecQueryer) []string {
	out := make([]string, len(database.Queries))
	for i, call := range database.Queries {
		switch {
		case strings.Contains(call.Query, "MAX(observed_at)"):
			out[i] = "max-generations"
		case strings.Contains(call.Query, "payload->>'repo_slug'"):
			out[i] = "slug-resolve"
		case strings.Contains(call.Query, "scope_id = ANY"):
			out[i] = "live-rows"
		default:
			out[i] = "unknown: " + call.Query[:60]
		}
	}
	return out
}

func TestPageSelectionStoreAnnotatesMixedPage(t *testing.T) {
	t.Parallel()

	old := batchNow.Add(-48 * time.Hour)
	script := batchScript{
		obs: map[string][][]any{
			"scope:a": {batchObsRow("scope:a", "sel", "selected", 3, "", "")},
			"scope:b": {batchObsRow("scope:b", "sel", "not_listed", 3, "renamed", "acme/b-v2")},
			"scope:c": {batchObsRow("scope:c", "sel", "not_listed", 3, "transferred", "archive/c")},
			"scope:d": {batchObsRow("scope:d", "sel", "not_listed", 3, "renamed", "acme/d-away")},
		},
		gens: map[string]time.Time{
			"scope:a": old, "scope:b": old, "scope:c": old, "scope:d": old,
		},
		slug: map[string]string{
			"acme/a": "scope:a", "acme/b-v2": "scope:a",
			"archive/c": "scope:c-off", "acme/d-away": "",
		},
	}
	// scope:c-off is an off-page selected scope; acme/d-away resolves to no
	// scope (renamed out).
	script.obs["scope:c-off"] = [][]any{batchObsRow("scope:c-off", "sel", "selected", 3, "", "")}
	database := &fake.ExecQueryer{Routes: []fake.Route{script.route}}
	store := postgres.NewPageSelectionStore(database)
	got, err := store.ReadPageSelections(context.Background(), []string{"scope:a", "scope:b", "scope:c", "scope:d", "scope:e"})
	if err != nil {
		t.Fatalf("ReadPageSelections() error = %v", err)
	}
	if want := []string{"live-rows", "max-generations", "slug-resolve", "live-rows"}; fmt.Sprintf("%v", batchQueries(database)) != fmt.Sprintf("%v", want) {
		t.Fatalf("queries = %v, want %v", batchQueries(database), want)
	}
	if got.ByScope["scope:a"].State != selection.AggregateSelected {
		t.Fatalf("scope:a = %+v, want selected", got.ByScope["scope:a"])
	}
	renamed := got.ByScope["scope:b"]
	if renamed.State != selection.AggregateNotSelected || renamed.Classification != selection.UpstreamRenamed ||
		renamed.SuccessorSlug != "acme/b-v2" || !renamed.SuccessorSelected {
		t.Fatalf("scope:b = %+v, want renamed with a selected on-page successor", renamed)
	}
	transferred := got.ByScope["scope:c"]
	if transferred.Classification != selection.UpstreamTransferred || transferred.SuccessorSlug != "archive/c" || !transferred.SuccessorSelected {
		t.Fatalf("scope:c = %+v, want transferred with a selected off-page successor", transferred)
	}
	away := got.ByScope["scope:d"]
	if away.Classification != selection.UpstreamRenamed || away.SuccessorSelected {
		t.Fatalf("scope:d = %+v, want renamed with an unselected (renamed-out) successor", away)
	}
	if unknown := got.ByScope["scope:e"]; unknown.State != selection.AggregateUnknown || unknown.Classification != "" {
		t.Fatalf("scope:e = %+v, want unknown with no annotation", unknown)
	}
}

func TestPageSelectionStoreSkipsUnneededReads(t *testing.T) {
	t.Parallel()

	old := batchNow.Add(-48 * time.Hour)
	newObs := func(scope string) [][]any {
		return [][]any{batchObsRow(scope, "sel", "selected", 3, "", "")}
	}
	t.Run("all selected skips generations and successors", func(t *testing.T) {
		t.Parallel()
		script := batchScript{
			obs:  map[string][][]any{"scope:a": newObs("scope:a"), "scope:b": newObs("scope:b")},
			gens: map[string]time.Time{"scope:a": old, "scope:b": old},
		}
		database := &fake.ExecQueryer{Routes: []fake.Route{script.route}}
		got, err := postgres.NewPageSelectionStore(database).ReadPageSelections(context.Background(), []string{"scope:a", "scope:b"})
		if err != nil {
			t.Fatalf("ReadPageSelections() error = %v", err)
		}
		if want := []string{"live-rows"}; fmt.Sprintf("%v", batchQueries(database)) != fmt.Sprintf("%v", want) {
			t.Fatalf("queries = %v, want %v: no scope needs rule (d) or a successor", batchQueries(database), want)
		}
		if got.ByScope["scope:a"].State != selection.AggregateSelected {
			t.Fatalf("scope:a = %+v, want selected", got.ByScope["scope:a"])
		}
	})
	t.Run("unclassified not_listed skips successors", func(t *testing.T) {
		t.Parallel()
		script := batchScript{
			obs: map[string][][]any{
				"scope:a": {batchObsRow("scope:a", "sel", "not_listed", 3, "", "")},
			},
			gens: map[string]time.Time{"scope:a": old},
		}
		database := &fake.ExecQueryer{Routes: []fake.Route{script.route}}
		got, err := postgres.NewPageSelectionStore(database).ReadPageSelections(context.Background(), []string{"scope:a"})
		if err != nil {
			t.Fatalf("ReadPageSelections() error = %v", err)
		}
		if want := []string{"live-rows", "max-generations"}; fmt.Sprintf("%v", batchQueries(database)) != fmt.Sprintf("%v", want) {
			t.Fatalf("queries = %v, want %v: generations but no successor check", batchQueries(database), want)
		}
		if summary := got.ByScope["scope:a"]; summary.State != selection.AggregateNotSelected || summary.Classification != "" {
			t.Fatalf("scope:a = %+v, want unannotated not_selected", summary)
		}
	})
}

// qaBatchScript extends the 802-scope QA corpus behind #7625 with probe
// verdicts: 777 scopes stay selected, 25 drop out of the listing (20
// transferred to the archive org, 2 renamed onto selected on-page
// successors, 1 renamed onto a selected off-page successor, 1 answering
// 404, 1 still present). Every generation predates the exclusion, so rule
// (d) never fires: the QA corpus behind #7625 kept no live generation for
// a scope the listing dropped.
func qaBatchScript() (batchScript, []string) {
	old := batchNow.Add(-72 * time.Hour)
	script := batchScript{obs: map[string][][]any{}, gens: map[string]time.Time{}, slug: map[string]string{}}
	var scopes []string
	addScope := func(scope, slug, state string, cycles int, classification, successor string) {
		script.obs[scope] = [][]any{batchObsRow(scope, "sel-live", state, cycles, classification, successor)}
		script.gens[scope] = old
		if slug != "" {
			script.slug[slug] = scope
		}
		scopes = append(scopes, scope)
	}
	for i := 1; i <= 777; i++ {
		scope := fmt.Sprintf("scope:stable-%d", i)
		addScope(scope, fmt.Sprintf("acme/stable-%d", i), "selected", 3, "", "")
	}
	// Renamed scope with a selected on-page successor, and the successor.
	addScope("scope:renamed-service", "acme/renamed-service", "not_listed", 3, "renamed", "acme/renamed-service-v2")
	addScope("scope:renamed-service-v2", "acme/renamed-service-v2", "selected", 2, "", "")
	addScope("scope:renamed-second", "acme/renamed-second", "not_listed", 3, "renamed", "acme/renamed-second-v2")
	addScope("scope:renamed-second-v2", "acme/renamed-second-v2", "selected", 2, "", "")
	// Renamed scope with a selected off-page successor: the successor scope
	// is not on the page, so the check costs one more bounded read.
	addScope("scope:renamed-off", "acme/renamed-off", "not_listed", 3, "renamed", "acme/renamed-off-v2")
	script.obs["scope:renamed-off-v2"] = [][]any{batchObsRow("scope:renamed-off-v2", "sel-live", "selected", 2, "", "")}
	script.gens["scope:renamed-off-v2"] = old
	script.slug["acme/renamed-off-v2"] = "scope:renamed-off-v2"
	// Transferred scopes resolve to no selected scope.
	for i := 1; i <= 20; i++ {
		scope := fmt.Sprintf("scope:transferred-%d", i)
		addScope(scope, fmt.Sprintf("acme/transferred-%d", i), "not_listed", 3, "transferred", fmt.Sprintf("archive/transferred-%d", i))
	}
	addScope("scope:gone-404", "acme/gone-404", "not_listed", 3, "not_found_or_inaccessible", "")
	addScope("scope:ghost", "acme/ghost", "not_listed", 3, "still_present", "")
	return script, scopes
}

func TestPageSelectionStoreQAShape(t *testing.T) {
	t.Parallel()

	script, scopes := qaBatchScript()
	if len(scopes) != 804 {
		t.Fatalf("QA page = %d scopes, want 804 (802 corpus plus 2 on-page successors)", len(scopes))
	}
	database := &fake.ExecQueryer{Routes: []fake.Route{script.route}}
	store := postgres.NewPageSelectionStore(database)
	got, err := store.ReadPageSelections(context.Background(), scopes)
	if err != nil {
		t.Fatalf("ReadPageSelections() error = %v", err)
	}
	// The page costs one live-rows read, one generations read, and one
	// successor resolution (slug resolve plus the off-page batch): every
	// statement is bounded by the page.
	if want := []string{"live-rows", "max-generations", "slug-resolve", "live-rows"}; fmt.Sprintf("%v", batchQueries(database)) != fmt.Sprintf("%v", want) {
		t.Fatalf("queries = %v, want %v", batchQueries(database), want)
	}
	byState := map[selection.Aggregate]int{}
	var falsePositives []string
	for scope, summary := range got.ByScope {
		byState[summary.State]++
		if strings.HasPrefix(scope, "scope:stable-") || strings.HasSuffix(scope, "-v2") {
			if summary.State != selection.AggregateSelected {
				falsePositives = append(falsePositives, scope)
			}
		}
	}
	if len(falsePositives) != 0 {
		t.Fatalf("false positives on listed scopes: %v", falsePositives)
	}
	if byState[selection.AggregateSelected] != 779 || byState[selection.AggregateNotSelected] != 25 {
		t.Fatalf("states = %+v, want 779 selected and 25 not_selected", byState)
	}
	renamed := got.ByScope["scope:renamed-service"]
	if renamed.Classification != selection.UpstreamRenamed || renamed.SuccessorSlug != "acme/renamed-service-v2" || !renamed.SuccessorSelected {
		t.Fatalf("renamed-service = %+v, want renamed with a selected successor", renamed)
	}
	off := got.ByScope["scope:renamed-off"]
	if off.Classification != selection.UpstreamRenamed || !off.SuccessorSelected {
		t.Fatalf("renamed-off = %+v, want renamed with a selected off-page successor", off)
	}
	transferred := got.ByScope["scope:transferred-1"]
	if transferred.Classification != selection.UpstreamTransferred || transferred.SuccessorSelected {
		t.Fatalf("transferred-1 = %+v, want transferred with an unselected successor", transferred)
	}
	notFound := got.ByScope["scope:gone-404"]
	if notFound.Classification != selection.UpstreamNotFoundOrInaccessible || notFound.SuccessorSlug != "" || notFound.SuccessorSelected {
		t.Fatalf("gone-404 = %+v, want not_found with no successor", notFound)
	}
	ghost := got.ByScope["scope:ghost"]
	if ghost.Classification != selection.UpstreamStillPresent || ghost.SuccessorSlug != "" {
		t.Fatalf("ghost = %+v, want still_present with no successor", ghost)
	}
}

func TestPageSelectionStoreFailsWholeReadOnError(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{FailWith: fmt.Errorf("postgres unavailable")}}}
	if _, err := postgres.NewPageSelectionStore(database).ReadPageSelections(context.Background(), []string{"scope:a"}); err == nil {
		t.Fatal("ReadPageSelections() error = nil, want the sub-query error to fail the whole read")
	}
	if _, err := postgres.NewPageSelectionStore(nil).ReadPageSelections(context.Background(), []string{"scope:a"}); err == nil {
		t.Fatal("ReadPageSelections(nil store) error = nil, want an error")
	}
	if _, err := postgres.NewPageSelectionStore(database).ReadPageSelectionsBySlug(context.Background(), nil); err != nil {
		t.Fatalf("ReadPageSelectionsBySlug(nil) error = %v, want no query for an empty page", err)
	}
}
