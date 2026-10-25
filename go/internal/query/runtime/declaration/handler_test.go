// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type listBody struct {
	Declarations []map[string]any `json:"declarations"`
	Count        int              `json:"count"`
	Limit        int              `json:"limit"`
	Truncated    bool             `json:"truncated"`
	NextCursor   string           `json:"next_cursor"`
	Excluded     struct {
		Total      int            `json:"total"`
		BySpecKind map[string]int `json:"by_spec_kind"`
	} `json:"excluded_from_comparison"`
	PartialReasons []string `json:"partial_reasons"`
}

func decodeList(t *testing.T, body []byte) listBody {
	t.Helper()
	var out listBody
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("json.Unmarshal(%s): %v", body, err)
	}
	return out
}

func TestListReturnsEveryDeclarationWithScopeDirAndNullVersionParts(t *testing.T) {
	t.Parallel()

	rangeRow := decl("repository:r_a", "web/package.json", 1, 0, "node", ">=18 <21", "range", nil)
	rangeRow["scope_dir"] = "web"
	rangeRow["range_lower_major"] = int64(18)
	graph := &fakeGraph{
		pageRows: []map[string]any{decl("repository:r_a", ".nvmrc", 1, 0, "node", "18.19.0", "exact", intp(18)), rangeRow},
		nameRows: []map[string]any{{"repo_id": "repository:r_a", "repo_name": "orders-api"}},
	}
	w := post(newMux(graph), `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if !graph.sawDeadline {
		t.Error("graph read had no server-side deadline")
	}
	got := decodeList(t, w.Body.Bytes())
	if got.Count != 2 || got.Limit != 50 || got.Truncated || got.NextCursor != "" {
		t.Fatalf("page = %+v", got)
	}
	if got.PartialReasons == nil || len(got.PartialReasons) != 0 {
		t.Errorf("partial_reasons = %#v, want an empty non-null list", got.PartialReasons)
	}
	first, second := got.Declarations[0], got.Declarations[1]
	if first["repo_name"] != "orders-api" || first["scope_dir"] != "." || first["version_major"] != float64(18) {
		t.Errorf("first row = %v", first)
	}
	if second["scope_dir"] != "web" {
		t.Errorf("second row scope_dir = %v, want web", second["scope_dir"])
	}
	for _, key := range []string{"version_major", "version_minor", "version_patch", "range_lower_major"} {
		v, present := second[key]
		if key == "range_lower_major" {
			if v != float64(18) {
				t.Errorf("range row %s = %v, want 18", key, v)
			}
			continue
		}
		if !present || v != nil {
			t.Errorf("range row %s = %v (present=%v), want an explicit null", key, v, present)
		}
	}
	if graph.callCount("AS excluded_count") != 0 {
		t.Error("a list with no version comparison ran the excluded count")
	}
}

func TestListMajorBelowNamesExcludedSpecsInPartialReasons(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{
		pageRows: []map[string]any{decl("repository:r_a", ".nvmrc", 1, 0, "node", "18.19.0", "exact", intp(18))},
		excludedRows: []map[string]any{
			{"spec_kind": "range", "excluded_count": int64(3)},
			{"spec_kind": "alias", "excluded_count": int64(2)},
			{"spec_kind": "unresolved", "excluded_count": int64(1)},
			{"spec_kind": "future_kind", "excluded_count": int64(4)},
		},
	}
	w := post(newMux(graph), `{"runtime":"node","major_below":20}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeList(t, w.Body.Bytes())
	if got.Excluded.Total != 10 || got.Excluded.BySpecKind["range"] != 3 || got.Excluded.BySpecKind["alias"] != 2 ||
		got.Excluded.BySpecKind["unresolved"] != 1 || got.Excluded.BySpecKind["other"] != 4 {
		t.Errorf("excluded = %+v", got.Excluded)
	}
	want := []string{
		"runtime_declaration_alias_specs_excluded",
		"runtime_declaration_other_specs_excluded",
		"runtime_declaration_range_specs_excluded",
		"runtime_declaration_unresolved_specs_excluded",
	}
	if strings.Join(got.PartialReasons, ",") != strings.Join(want, ",") {
		t.Errorf("partial_reasons = %v, want %v", got.PartialReasons, want)
	}
	page := graph.callContaining(t, "ORDER BY d.repo_id")
	if page.params["runtime"] != "node" || page.params["below_0"] != 20 {
		t.Errorf("page params = %v", page.params)
	}
}

func TestListVersionBelowNamesPartialSpecsTooImpreciseToCompare(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{excludedRows: []map[string]any{{"spec_kind": "partial", "excluded_count": int64(5)}}}
	w := post(newMux(graph), `{"runtime":"node","version_below":"20.10"}`)
	got := decodeList(t, w.Body.Bytes())
	if len(got.PartialReasons) != 1 || got.PartialReasons[0] != "runtime_declaration_partial_specs_excluded" {
		t.Errorf("partial_reasons = %v", got.PartialReasons)
	}
}

func TestListTruncatesAtLimitAndIssuesACursorThatResumesAfterTheLastRow(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		decl("repository:r_a", ".nvmrc", 1, 0, "node", "18", "partial", intp(18)),
		decl("repository:r_b", ".nvmrc", 1, 0, "node", "16", "partial", intp(16)),
		decl("repository:r_c", ".nvmrc", 1, 0, "node", "14", "partial", intp(14)),
	}
	graph := &fakeGraph{pageRows: rows}
	w := post(newMux(graph), `{"limit":2}`)
	got := decodeList(t, w.Body.Bytes())
	if got.Count != 2 || !got.Truncated || got.NextCursor == "" {
		t.Fatalf("page = %+v", got)
	}
	if page := graph.callContaining(t, "ORDER BY d.repo_id"); page.params["limit"] != 3 {
		t.Errorf("limit param = %v, want limit+1 = 3", page.params["limit"])
	}
	key, err := decodeCursor(got.NextCursor)
	if err != nil {
		t.Fatalf("next_cursor does not decode: %v", err)
	}
	if key.RepoID != "repository:r_b" || key.RelativePath != ".nvmrc" || key.LineNumber != 1 || key.Runtime != "node" || key.OrderIndex != 0 {
		t.Errorf("next_cursor key = %+v, want the last returned row", key)
	}

	resumed := &fakeGraph{pageRows: rows[2:]}
	w = post(newMux(resumed), `{"limit":2,"cursor":"`+got.NextCursor+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("resume status = %d; body = %s", w.Code, w.Body.String())
	}
	resumedPage := resumed.callContaining(t, "ORDER BY d.repo_id")
	if resumedPage.params["cursor_repo_id"] != "repository:r_b" {
		t.Errorf("resume params = %v", resumedPage.params)
	}
	last := decodeList(t, w.Body.Bytes())
	if last.Truncated || last.NextCursor != "" || last.Count != 1 {
		t.Errorf("last page = %+v", last)
	}
}

func TestListRepositoryNameFailureDegradesWithNamedReason(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{
		pageRows: []map[string]any{decl("repository:r_a", ".nvmrc", 1, 0, "node", "18", "partial", intp(18))},
		nameErr:  stubError{},
	}
	w := post(newMux(graph), `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeList(t, w.Body.Bytes())
	if len(got.PartialReasons) != 1 || got.PartialReasons[0] != "runtime_declaration_repository_names_unavailable" {
		t.Errorf("partial_reasons = %v", got.PartialReasons)
	}
	if got.Count != 1 || got.Declarations[0]["repo_id"] != "repository:r_a" {
		t.Errorf("rows were dropped with the names: %+v", got)
	}
}

// stubError is a stand-in graph failure.
type stubError struct{}

func (stubError) Error() string { return "name lookup failed" }

func TestListEmptyResultIsAnEmptyListNotNull(t *testing.T) {
	t.Parallel()

	w := post(newMux(&fakeGraph{}), `{"runtime":"cobol"}`)
	if !strings.Contains(w.Body.String(), `"declarations":[]`) {
		t.Errorf("body = %s, want declarations:[]", w.Body.String())
	}
}

func TestListEnvelopeCarriesTruth(t *testing.T) {
	t.Parallel()

	req := httpReq(`{}`)
	req.Header.Set("Accept", "application/eshu.envelope+json")
	w := serve(newMux(&fakeGraph{}), req)
	var env struct {
		Data  json.RawMessage `json:"data"`
		Truth struct {
			Level      string `json:"level"`
			Capability string `json:"capability"`
			Basis      string `json:"basis"`
		} `json:"truth"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Truth.Level != "exact" || env.Truth.Capability != Capability || env.Truth.Basis != "authoritative_graph" || env.Error != nil || len(env.Data) == 0 {
		t.Errorf("envelope = %s", w.Body.String())
	}
}
