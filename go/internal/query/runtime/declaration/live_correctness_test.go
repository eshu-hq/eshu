// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package declaration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// liveCorpus is a small corpus whose every answer is derivable by hand. It
// covers each spec kind, the scope_dir of a nested pin file, several
// runtimes and source kinds, and a tie on (repo, path, line) broken by
// order_index.
func liveCorpus() []map[string]any {
	a, b, c, d, e, f := liveRepoID(1), liveRepoID(2), liveRepoID(3), liveRepoID(4), liveRepoID(5), liveRepoID(6)
	return []map[string]any{
		liveRow(a, ".nvmrc", 1, 0, "node", "nvmrc", "18.19.0", "exact", ".", 18, 19, 0),
		liveRow(a, "services/web/.nvmrc", 1, 0, "node", "nvmrc", "20", "partial", "services/web", 20),
		liveRow(a, ".tool-versions", 1, 0, "node", "tool_versions", "16.20.2", "exact", ".", 16, 20, 2),
		liveRow(a, ".tool-versions", 1, 1, "node", "tool_versions", "lts/iron", "alias", "."),
		liveRow(a, ".tool-versions", 2, 0, "python", "tool_versions", "3.12", "partial", ".", 3, 12),
		liveRow(b, "package.json", 1, 0, "node", "package_json_engines", ">=18 <21", "range", "."),
		liveRow(b, ".nvmrc", 1, 0, "node", "nvmrc", "${NODE_VERSION}", "unresolved", "."),
		liveRow(b, ".tool-versions", 1, 0, "node", "tool_versions", "22.1.0", "exact", ".", 22, 1, 0),
		liveRow(c, ".nvmrc", 1, 0, "node", "nvmrc", "20.10.0", "exact", ".", 20, 10, 0),
		liveRow(c, "go.mod", 3, 0, "go", "go_mod_go", "1.21", "partial", ".", 1, 21),
		liveRow(d, ".nvmrc", 1, 0, "node", "nvmrc", "14", "partial", ".", 14),
		liveRow(d, ".tool-versions", 1, 0, "node", "tool_versions", "node", "alias", "."),
		liveRow(e, ".nvmrc", 1, 0, "node", "nvmrc", "20.9.5", "exact", ".", 20, 9, 5),
		liveRow(e, ".tool-versions", 1, 0, "node", "tool_versions", "20.10", "partial", ".", 20, 10),
		liveRow(f, ".python-version", 1, 0, "python", "tool_versions", "3.9.18", "exact", ".", 3, 9, 18),
	}
}

func setupLiveCorpus(t *testing.T) (liveStore, http.Handler) {
	t.Helper()
	store := openLiveStore(t)
	store.reset(t)
	t.Cleanup(func() { store.reset(t) })
	store.seed(t, liveCorpus(), 500)
	store.write(t, `MATCH (r:Repository) WHERE r.id STARTS WITH 'repository:live8817-' SET r.name = 'name-' + r.id`, nil)
	mux := http.NewServeMux()
	(&Handler{Neo4j: store.reader}).Mount(mux)
	return store, mux
}

// liveKey renders a row as "repo-number:path:line:runtime:order:raw" for
// comparison against hand-derived expectations.
func liveKey(row map[string]any) string {
	return strings.Join([]string{
		strings.TrimPrefix(row["repo_id"].(string), "repository:live8817-"),
		row["relative_path"].(string),
		jsonNumber(row["line_number"]), row["runtime"].(string), jsonNumber(row["order_index"]), row["raw_spec"].(string),
	}, "|")
}

func jsonNumber(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func liveKeys(rows []map[string]any) []string {
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = liveKey(row)
	}
	return keys
}

func liveQuery(t *testing.T, h http.Handler, body string) listBody {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/runtime-declarations/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: status %d: %s", body, rec.Code, rec.Body.String())
	}
	return decodeList(t, rec.Body.Bytes())
}

func requireKeys(t *testing.T, label string, got listBody, want ...string) {
	t.Helper()
	if keys := liveKeys(got.Declarations); strings.Join(keys, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s rows =\n  %s\nwant\n  %s", label, strings.Join(keys, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestLiveMajorBelowListsOnlyPlaceableDeclarationsAndNamesTheRest(t *testing.T) {
	_, h := setupLiveCorpus(t)
	got := liveQuery(t, h, `{"runtime":"node","major_below":20}`)
	requireKeys(t, "node below 20", got,
		"00001|.nvmrc|1|node|0|18.19.0",
		"00001|.tool-versions|1|node|0|16.20.2",
		"00004|.nvmrc|1|node|0|14",
	)
	if got.Excluded.Total != 4 || got.Excluded.BySpecKind["alias"] != 2 || got.Excluded.BySpecKind["range"] != 1 || got.Excluded.BySpecKind["unresolved"] != 1 {
		t.Errorf("excluded = %+v, want alias 2, range 1, unresolved 1", got.Excluded)
	}
	want := []string{"runtime_declaration_alias_specs_excluded", "runtime_declaration_range_specs_excluded", "runtime_declaration_unresolved_specs_excluded"}
	if strings.Join(got.PartialReasons, ",") != strings.Join(want, ",") {
		t.Errorf("partial_reasons = %v, want %v", got.PartialReasons, want)
	}
	for _, row := range got.Declarations {
		if row["repo_name"] != "name-"+row["repo_id"].(string) {
			t.Errorf("repo_name = %v for %v", row["repo_name"], row["repo_id"])
		}
	}
}

func TestLiveVersionBelowComparesMinorAndPatchAndNamesImpreciseSpecs(t *testing.T) {
	_, h := setupLiveCorpus(t)
	minor := liveQuery(t, h, `{"runtime":"node","version_below":"20.10"}`)
	requireKeys(t, "node below 20.10", minor,
		"00001|.nvmrc|1|node|0|18.19.0",
		"00001|.tool-versions|1|node|0|16.20.2",
		"00004|.nvmrc|1|node|0|14",
		"00005|.nvmrc|1|node|0|20.9.5",
	)
	// "20" (services/web) has no minor, so it cannot be placed against 20.10.
	if minor.Excluded.BySpecKind["partial"] != 1 {
		t.Errorf("partial exclusions = %v, want 1 (the bare 20)", minor.Excluded.BySpecKind)
	}
	patch := liveQuery(t, h, `{"runtime":"node","version_below":"20.10.1"}`)
	requireKeys(t, "node below 20.10.1", patch,
		"00001|.nvmrc|1|node|0|18.19.0",
		"00001|.tool-versions|1|node|0|16.20.2",
		"00003|.nvmrc|1|node|0|20.10.0",
		"00004|.nvmrc|1|node|0|14",
		"00005|.nvmrc|1|node|0|20.9.5",
	)
	// A declaration equal to the bound is not below it.
	equal := liveQuery(t, h, `{"runtime":"node","version_below":"20.10.0"}`)
	requireKeys(t, "node below 20.10.0", equal,
		"00001|.nvmrc|1|node|0|18.19.0",
		"00001|.tool-versions|1|node|0|16.20.2",
		"00004|.nvmrc|1|node|0|14",
		"00005|.nvmrc|1|node|0|20.9.5",
	)
	// 20 and 20.10 lack the patch the bound names.
	if patch.Excluded.BySpecKind["partial"] != 2 {
		t.Errorf("partial exclusions = %v, want 2 (20 and 20.10)", patch.Excluded.BySpecKind)
	}
}

func TestLiveRowsCarryScopeDirAndFiltersCompose(t *testing.T) {
	_, h := setupLiveCorpus(t)
	nested := liveQuery(t, h, `{"runtime":"node","source_kind":"nvmrc","repo_ids":["`+liveRepoID(1)+`"]}`)
	requireKeys(t, "repo 1 nvmrc node", nested, "00001|.nvmrc|1|node|0|18.19.0", "00001|services/web/.nvmrc|1|node|0|20")
	if nested.Declarations[1]["scope_dir"] != "services/web" || nested.Declarations[0]["scope_dir"] != "." {
		t.Errorf("scope_dir = %v / %v", nested.Declarations[0]["scope_dir"], nested.Declarations[1]["scope_dir"])
	}
	if none := liveQuery(t, h, `{"runtime":"cobol"}`); none.Count != 0 || none.Truncated {
		t.Errorf("unknown runtime = %+v, want empty", none)
	}
	if nokind := liveQuery(t, h, `{"source_kind":"go_mod_go"}`); nokind.Count != 1 || nokind.Declarations[0]["runtime"] != "go" {
		t.Errorf("source_kind go_mod_go = %+v", nokind)
	}
}

func TestLivePagingWalksTheWholeKeyOrderWithoutGapsOrRepeats(t *testing.T) {
	_, h := setupLiveCorpus(t)
	for _, body := range []string{`{}`, `{"runtime":"node"}`, `{"runtime":"node","major_below":23}`} {
		full := liveQuery(t, h, strings.TrimSuffix(body, "}")+withLimit(body, 200))
		want := liveKeys(full.Declarations)
		var walked []string
		cursor := ""
		for page := 0; page < 40; page++ {
			req := strings.TrimSuffix(body, "}") + withLimit(body, 2)
			if cursor != "" {
				req = strings.TrimSuffix(req, "}") + `,"cursor":"` + cursor + `"}`
			}
			got := liveQuery(t, h, req)
			walked = append(walked, liveKeys(got.Declarations)...)
			if !got.Truncated {
				if got.NextCursor != "" {
					t.Fatalf("%s: last page issued a cursor", body)
				}
				break
			}
			if got.NextCursor == "" || got.Count != 2 {
				t.Fatalf("%s: truncated page without cursor or short: %+v", body, got)
			}
			cursor = got.NextCursor
		}
		if strings.Join(walked, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: paged walk =\n  %s\nwant (one page of 200)\n  %s", body, strings.Join(walked, "\n  "), strings.Join(want, "\n  "))
		}
	}
}

// withLimit renders the tail that adds a limit to a JSON object body.
func withLimit(body string, limit int) string {
	sep := ","
	if body == "{}" {
		sep = ""
	}
	return sep + `"limit":` + jsonNumber(limit) + `}`
}

func TestLiveScopedCallerSeesOnlyGrantedRepositories(t *testing.T) {
	_, h := setupLiveCorpus(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/runtime-declarations/query", strings.NewReader(`{"runtime":"node","major_below":23}`))
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "t", WorkspaceID: "t", SubjectClass: "team",
		SubjectIDHash: "sha256:t", PolicyRevisionHash: "sha256:p",
		AllowedRepositoryIDs: []string{liveRepoID(3), liveRepoID(4)},
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := decodeList(t, rec.Body.Bytes())
	requireKeys(t, "scoped node below 23", got, "00003|.nvmrc|1|node|0|20.10.0", "00004|.nvmrc|1|node|0|14")
	// Only repo 4's alias is in the grant; repo 1, 2 and 5 exclusions must not
	// be counted or named for this caller.
	if got.Excluded.Total != 1 || got.Excluded.BySpecKind["alias"] != 1 {
		t.Errorf("scoped excluded = %+v, want only repo 4's alias", got.Excluded)
	}
}

func TestLiveSectionSummarizesOneRepository(t *testing.T) {
	store, _ := setupLiveCorpus(t)
	section, err := LoadSection(context.Background(), store.reader, nil, liveRepoID(1))
	if err != nil {
		t.Fatalf("LoadSection() error = %v", err)
	}
	if section.Count != 5 || len(section.Runtimes) != 2 {
		t.Fatalf("section = %+v", section)
	}
	node, python := section.Runtimes[0], section.Runtimes[1]
	if node.Runtime != "node" || node.DeclarationCount != 4 || node.ResolvedCount != 3 || node.LowestResolvedMajor == nil || *node.LowestResolvedMajor != 16 {
		t.Errorf("node = %+v, want 4 declarations, 3 resolved, lowest major 16", node)
	}
	if python.Runtime != "python" || python.DeclarationCount != 1 || *python.LowestResolvedMajor != 3 {
		t.Errorf("python = %+v", python)
	}
	empty, err := LoadSection(context.Background(), store.reader, nil, "repository:live8817-none")
	if err != nil || empty.Count != 0 || len(empty.Runtimes) != 0 {
		t.Errorf("repository with no declarations = %+v, %v", empty, err)
	}
}
