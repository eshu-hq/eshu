// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

// fakePageSelections serves canned aggregates keyed by scope id.
type fakePageSelections struct {
	byScope map[string]selection.Summary
	bySlug  map[string]string
	err     error
}

func (f fakePageSelections) ReadPageSelectionsBySlug(_ context.Context, slugs []string) (selection.PageSelections, error) {
	if f.err != nil {
		return selection.PageSelections{}, f.err
	}
	out := selection.PageSelections{ByScope: map[string]selection.Summary{}, ScopeBySlug: map[string]string{}}
	for _, slug := range slugs {
		if scope, ok := f.bySlug[strings.ToLower(slug)]; ok {
			out.ScopeBySlug[strings.ToLower(slug)] = scope
			out.ByScope[scope] = f.byScope[scope]
		}
	}
	return out, nil
}

var annotationNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func annotationFixture() (fakePageSelections, map[string]selection.Summary) {
	byScope := map[string]selection.Summary{
		"scope:gone": {
			State: selection.AggregateNotSelected, Reason: selection.StateNotListed,
			StateSince: annotationNow.Add(-48 * time.Hour), EvaluatedAt: annotationNow, LiveSelectorCount: 1,
			Classification: selection.UpstreamTransferred, SuccessorSlug: "archive/gone",
		},
	}
	return fakePageSelections{
		byScope: byScope,
		bySlug:  map[string]string{"acme/gone": "scope:gone", "acme/staying": "scope:staying"},
	}, byScope
}

func contentHandler(selections PageSelectionReader) *Handler {
	return &Handler{
		Content: content.FakePortContentStore{
			Repositories: []querycontract.RepositoryCatalogEntry{
				{ID: "repository:gone", Name: "gone", RepoSlug: "acme/gone", HasRemote: true},
				{ID: "repository:staying", Name: "staying", RepoSlug: "acme/staying", HasRemote: true},
			},
		},
		Selections: selections,
		Profile:    querycontract.ProfileLocalLightweight,
	}
}

func listRepositoriesBody(t *testing.T, handler *Handler) []any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.listRepositories(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	repositories, ok := body["repositories"].([]any)
	if !ok {
		t.Fatalf("repositories = %#v, want a list", body["repositories"])
	}
	return repositories
}

func TestListRepositoriesAnnotatesEntriesFromContent(t *testing.T) {
	t.Parallel()

	store, _ := annotationFixture()
	repositories := listRepositoriesBody(t, contentHandler(store))
	if len(repositories) != 2 {
		t.Fatalf("repositories = %d rows, want 2: annotation never changes row counts", len(repositories))
	}
	gone := repositories[0].(map[string]any)
	block, ok := gone["selection"].(map[string]any)
	if !ok {
		t.Fatalf("gone selection = %#v, want the aggregate block", gone["selection"])
	}
	if block["state"] != "not_selected" || block["reason"] != "not_listed" ||
		block["classification"] != "transferred" || block["successor_slug"] != "archive/gone" {
		t.Fatalf("gone selection = %#v, want the transferred verdict", block)
	}
	staying := repositories[1].(map[string]any)
	unknown, ok := staying["selection"].(map[string]any)
	if !ok || unknown["state"] != "unknown" || unknown["classification"] != nil {
		t.Fatalf("staying selection = %#v, want unknown with null classification", staying["selection"])
	}
}

func TestListRepositoriesAnnotationIsAdditive(t *testing.T) {
	t.Parallel()

	plain := listRepositoriesBody(t, contentHandler(nil))
	store, _ := annotationFixture()
	annotated := listRepositoriesBody(t, contentHandler(store))
	if len(plain) != len(annotated) {
		t.Fatalf("row counts differ: %d without observations, %d with", len(plain), len(annotated))
	}
	for i := range plain {
		withoutSelection := map[string]any{}
		for key, value := range plain[i].(map[string]any) {
			if key != "selection" {
				withoutSelection[key] = value
			}
		}
		withSelection := map[string]any{}
		for key, value := range annotated[i].(map[string]any) {
			if key != "selection" {
				withSelection[key] = value
			}
		}
		if fmt.Sprintf("%v", withoutSelection) != fmt.Sprintf("%v", withSelection) {
			t.Fatalf("row %d differs beyond selection: %#v vs %#v", i, withoutSelection, withSelection)
		}
		if _, ok := annotated[i].(map[string]any)["selection"]; !ok {
			t.Fatalf("row %d has no selection block", i)
		}
	}
}

// TestListRepositoriesByteIdenticalWithoutMatchingObservations pins the
// #8454 non-interference boundary: observation rows that resolve to no page
// scope render byte-identical bytes to no store at all. Row counts and
// ResolveRepository outcomes (which never consult the annotation store)
// stay identical with and without observation rows by the same token: the
// annotation only adds the per-entry block on the list path.
func TestListRepositoriesByteIdenticalWithoutMatchingObservations(t *testing.T) {
	t.Parallel()

	serve := func(selections PageSelectionReader) string {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
		rec := httptest.NewRecorder()
		contentHandler(selections).listRepositories(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	withoutStore := serve(nil)
	withUnmatchedRows := serve(fakePageSelections{byScope: map[string]selection.Summary{
		"scope:elsewhere": {State: selection.AggregateNotSelected, Reason: selection.StateNotListed},
	}, bySlug: map[string]string{"acme/elsewhere": "scope:elsewhere"}})
	if withoutStore != withUnmatchedRows {
		t.Fatalf("list bytes differ with non-resolving observation rows:\n%s\nvs\n%s", withoutStore, withUnmatchedRows)
	}
}

func TestListRepositoriesAnnotationFailureFailsList(t *testing.T) {
	t.Parallel()

	handler := contentHandler(fakePageSelections{err: fmt.Errorf("postgres unavailable")})
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.listRepositories(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = 200 with body %s; entries never serve with evidence silently missing", rec.Body.String())
	}
}
