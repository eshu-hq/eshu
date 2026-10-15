// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// #7247: the language-query enrichment used to read a SECOND, independent
// content page (search_entities_by_language_and_type, ordered by
// relative_path/start_line/entity_name) and merge it into the graph page
// (ordered by relative_path/name, case-sensitive CONTAINS) by path/type/name/
// line key. The two pages are different sets, so most graph rows got no content
// metadata. These tests pin the replacement: for a label whose graph id IS the
// content primary key, one id-anchored lookup serves every graph row.

const anchoredTestRepo = "repository:r_anchored"

// anchoredMetadataStore is a content-store double that implements both the
// grant-bound page read and the id-anchored lookup, recording each call.
type anchoredMetadataStore struct {
	content.FakePortContentStore

	// page answers every SearchEntitiesByLanguageAndTypeForAccess call.
	page []querycontract.EntityContent
	// byID answers ListEntityMetadataByIDsForAccess for each requested id it
	// holds. The double does not apply the repository scope itself: the
	// statement-shape test in package query pins the SQL grant filter, and the
	// handler must not trust a row whose repository differs from the graph row.
	byID map[string]querycontract.EntityContent

	mu        sync.Mutex
	pageReads []taxonomy.LanguageEntitySearch
	idLookups []taxonomy.LanguageEntityIDLookup
}

func (s *anchoredMetadataStore) SearchEntitiesByLanguageAndTypeForAccess(
	_ context.Context,
	search taxonomy.LanguageEntitySearch,
) ([]querycontract.EntityContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageReads = append(s.pageReads, search)
	return s.page, nil
}

func (s *anchoredMetadataStore) ListEntityMetadataByIDsForAccess(
	_ context.Context,
	lookup taxonomy.LanguageEntityIDLookup,
) ([]querycontract.EntityContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idLookups = append(s.idLookups, lookup)
	rows := make([]querycontract.EntityContent, 0, len(lookup.EntityIDs))
	for _, id := range lookup.EntityIDs {
		if row, ok := s.byID[id]; ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// pageOnlyMetadataStore is the pre-#7247 store shape: it implements only the
// page read, so the enrichment must keep issuing that read.
type pageOnlyMetadataStore struct {
	content.FakePortContentStore
	page []querycontract.EntityContent
}

func (s *pageOnlyMetadataStore) SearchEntitiesByLanguageAndTypeForAccess(
	_ context.Context,
	_ taxonomy.LanguageEntitySearch,
) ([]querycontract.EntityContent, error) {
	return s.page, nil
}

// anchoredGraphRows builds n graph rows of one label, in the graph page's
// order (relative_path, name), each with a distinct content-shaped id.
func anchoredGraphRows(label string, n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := range n {
		rows = append(rows, map[string]any{
			"entity_id":  fmt.Sprintf("content-entity:e_graph_%03d", i),
			"name":       fmt.Sprintf("graphFn%03d", i),
			"labels":     []any{label},
			"file_path":  fmt.Sprintf("src/graph_%03d.php", i),
			"repo_id":    anchoredTestRepo,
			"repo_name":  "anchored",
			"language":   "php",
			"start_line": int64(i + 1),
			"end_line":   int64(i + 5),
		})
	}
	return rows
}

// anchoredContentRow is the content row a graph row's id names.
func anchoredContentRow(graphRow map[string]any, entityType string, metadata map[string]any) querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityID:     graphRow["entity_id"].(string),
		RepoID:       graphRow["repo_id"].(string),
		RelativePath: graphRow["file_path"].(string),
		EntityType:   entityType,
		EntityName:   graphRow["name"].(string),
		StartLine:    int(graphRow["start_line"].(int64)),
		EndLine:      int(graphRow["end_line"].(int64)),
		Language:     "php",
		Metadata:     metadata,
	}
}

// disjointContentPage is a content page that shares no row with the graph
// page: different paths, names and ids, as the two independent orderings
// produce on a large repository.
func disjointContentPage(entityType string, n int) []querycontract.EntityContent {
	rows := make([]querycontract.EntityContent, 0, n)
	for i := range n {
		rows = append(rows, querycontract.EntityContent{
			EntityID:     fmt.Sprintf("content-entity:e_page_%03d", i),
			RepoID:       anchoredTestRepo,
			RelativePath: fmt.Sprintf("aaa/page_%03d.php", i),
			EntityType:   entityType,
			EntityName:   fmt.Sprintf("pageFn%03d", i),
			StartLine:    i + 1,
			EndLine:      i + 3,
			Language:     "php",
			Metadata:     map[string]any{"docstring": "page row"},
		})
	}
	return rows
}

func anchoredDocstring(id string) string { return "content metadata for " + id }

// newAnchoredStore seeds a store whose id lookup has metadata for every graph
// row and whose page read is disjoint from the graph page.
func newAnchoredStore(graphRows []map[string]any, entityType string) *anchoredMetadataStore {
	store := &anchoredMetadataStore{
		page: disjointContentPage(entityType, len(graphRows)),
		byID: make(map[string]querycontract.EntityContent, len(graphRows)),
	}
	for _, row := range graphRows {
		id := row["entity_id"].(string)
		store.byID[id] = anchoredContentRow(row, entityType, map[string]any{"docstring": anchoredDocstring(id)})
	}
	return store
}

func anchoredGrant() codequery.LanguageQueryGrant {
	return codequery.LanguageQueryGrant{AllowedRepositoryIDs: []string{anchoredTestRepo}}
}

// TestLanguageMetadataAnchoredLookupEnrichesEveryGraphRow is the #7247
// regression: with a content page disjoint from the graph page, every graph
// row of a content-id-anchored label must still carry its own content
// metadata, from one id-keyed lookup and no page read.
func TestLanguageMetadataAnchoredLookupEnrichesEveryGraphRow(t *testing.T) {
	t.Parallel()

	graphRows := anchoredGraphRows("Function", languageQueryMaxLimit)
	store := newAnchoredStore(graphRows, "Function")
	handler := &Handler{
		Neo4j:   &testutil.MockLanguageQueryGraphReader{Rows: graphRows},
		Content: store,
		Profile: querycontract.ProfileProduction,
	}

	results, basis, err := handler.queryByLanguage(t.Context(), "php", "Function", "", "", languageQueryMaxLimit, anchoredGrant())
	if err != nil {
		t.Fatalf("queryByLanguage() error = %v", err)
	}
	if got, want := len(results), len(graphRows); got != want {
		t.Fatalf("results = %d rows, want %d", got, want)
	}
	missing := 0
	for _, result := range results {
		id, _ := result["entity_id"].(string)
		metadata, _ := result["metadata"].(map[string]any)
		if got, want := metadata["docstring"], anchoredDocstring(id); got != want {
			missing++
		}
	}
	if missing != 0 {
		t.Fatalf("%d of %d graph rows lack their own content metadata; the enrichment is not keyed to the graph rows", missing, len(results))
	}
	if basis != querycontract.TruthBasisHybrid {
		t.Fatalf("basis = %q, want %q", basis, querycontract.TruthBasisHybrid)
	}
	if got := len(store.pageReads); got != 0 {
		t.Fatalf("page reads = %d, want 0: every row was anchorable, so the filtered page read must not be issued", got)
	}
	if got := len(store.idLookups); got != 1 {
		t.Fatalf("id lookups = %d, want exactly 1", got)
	}
	lookup := store.idLookups[0]
	if got, want := len(lookup.EntityIDs), len(graphRows); got != want {
		t.Fatalf("id lookup carried %d ids, want %d (one per graph row)", got, want)
	}
	if !reflect.DeepEqual(lookup.AllowedRepositoryIDs, []string{anchoredTestRepo}) {
		t.Fatalf("id lookup grant = %v, want the caller's grant %v", lookup.AllowedRepositoryIDs, []string{anchoredTestRepo})
	}
}

// TestLanguageMetadataAnchoredLookupCarriesTheRepositoryScope pins that a
// repo-scoped request binds the id lookup to that repository.
func TestLanguageMetadataAnchoredLookupCarriesTheRepositoryScope(t *testing.T) {
	t.Parallel()

	graphRows := anchoredGraphRows("Class", 3)
	store := newAnchoredStore(graphRows, "Class")
	handler := &Handler{Neo4j: &testutil.MockLanguageQueryGraphReader{Rows: graphRows}, Content: store}

	grant := codequery.LanguageQueryGrant{AllowedRepositoryIDs: []string{anchoredTestRepo}}
	if _, _, err := handler.queryByLanguage(t.Context(), "php", "Class", "", anchoredTestRepo, 10, grant); err != nil {
		t.Fatalf("queryByLanguage() error = %v", err)
	}
	if got := len(store.idLookups); got != 1 {
		t.Fatalf("id lookups = %d, want 1", got)
	}
	if got := store.idLookups[0].RepoID; got != anchoredTestRepo {
		t.Fatalf("id lookup RepoID = %q, want %q", got, anchoredTestRepo)
	}
}

// TestLanguageMetadataAnchoredLookupRejectsAnotherRepositorysRow pins the
// handler-side half of the grant: a content row the store returns for a graph
// row's id but attributed to another repository is never merged.
func TestLanguageMetadataAnchoredLookupRejectsAnotherRepositorysRow(t *testing.T) {
	t.Parallel()

	graphRows := anchoredGraphRows("Function", 2)
	store := newAnchoredStore(graphRows, "Function")
	foreignID := graphRows[1]["entity_id"].(string)
	foreign := store.byID[foreignID]
	foreign.RepoID = "repository:r_not_granted"
	foreign.Metadata = map[string]any{"docstring": "ungranted repository metadata"}
	store.byID[foreignID] = foreign
	handler := &Handler{Neo4j: &testutil.MockLanguageQueryGraphReader{Rows: graphRows}, Content: store}

	results, _, err := handler.queryByLanguage(t.Context(), "php", "Function", "", "", 10, anchoredGrant())
	if err != nil {
		t.Fatalf("queryByLanguage() error = %v", err)
	}
	for _, result := range results {
		metadata, _ := result["metadata"].(map[string]any)
		if metadata["docstring"] == "ungranted repository metadata" {
			t.Fatalf("row %v merged metadata from a content row in another repository", result["entity_id"])
		}
	}
	first, _ := results[0]["metadata"].(map[string]any)
	if got, want := first["docstring"], anchoredDocstring(graphRows[0]["entity_id"].(string)); got != want {
		t.Fatalf("granted row docstring = %v, want %v", got, want)
	}
}

// TestLanguageMetadataNonAnchoredLabelsKeepThePageRead pins the boundary of
// the change: a label whose graph id is not the content primary key -- Module
// (its own MERGE key), Variable (dependency-identity ids differ, #7247 proof),
// and SqlTable (outside the projector's derived-id set) -- keeps today's page
// read and never issues the id lookup.
func TestLanguageMetadataNonAnchoredLabelsKeepThePageRead(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"Module", "Variable", "SqlTable"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			graphRows := anchoredGraphRows(label, 4)
			store := newAnchoredStore(graphRows, label)
			handler := &Handler{Neo4j: &testutil.MockLanguageQueryGraphReader{Rows: graphRows}, Content: store}
			if _, _, err := handler.queryByLanguage(t.Context(), "php", label, "", "", 10, anchoredGrant()); err != nil {
				t.Fatalf("queryByLanguage() error = %v", err)
			}
			if got := len(store.idLookups); got != 0 {
				t.Fatalf("%s issued %d id lookups, want 0: its graph id is not the content primary key", label, got)
			}
			if got := len(store.pageReads); got != 1 {
				t.Fatalf("%s issued %d page reads, want 1", label, got)
			}
		})
	}
}

// TestLanguageMetadataDependencyVariableNeverTakesAnIDMatch is the negative
// case for the #7247 Variable correction. A dependency-identity Variable's
// content id is minted by CanonicalDependencyEntityID, not the line-keyed id
// the graph carries, so an id-keyed merge could attach a DIFFERENT row's
// metadata. The store here answers the graph id with wrong metadata and the
// page read with the row's real metadata: the merge must come from the page.
func TestLanguageMetadataDependencyVariableNeverTakesAnIDMatch(t *testing.T) {
	t.Parallel()

	graphRows := anchoredGraphRows("Variable", 1)
	graphRow := graphRows[0]
	store := &anchoredMetadataStore{
		byID: map[string]querycontract.EntityContent{
			graphRow["entity_id"].(string): anchoredContentRow(graphRow, "Variable", map[string]any{"docstring": "wrong row by id"}),
		},
	}
	pageRow := anchoredContentRow(graphRow, "Variable", map[string]any{"docstring": "dependency row by key", "section": "require"})
	pageRow.EntityID = "content-entity:dependency_minted_id"
	store.page = []querycontract.EntityContent{pageRow}
	handler := &Handler{Neo4j: &testutil.MockLanguageQueryGraphReader{Rows: graphRows}, Content: store}

	results, _, err := handler.queryByLanguage(t.Context(), "php", "Variable", "", "", 10, anchoredGrant())
	if err != nil {
		t.Fatalf("queryByLanguage() error = %v", err)
	}
	metadata, _ := results[0]["metadata"].(map[string]any)
	if got, want := metadata["docstring"], "dependency row by key"; got != want {
		t.Fatalf("Variable docstring = %v, want %v (an id match would merge another row)", got, want)
	}
	if got := len(store.idLookups); got != 0 {
		t.Fatalf("Variable issued %d id lookups, want 0", got)
	}
}

// TestLanguageMetadataAnchoredLookupKeepsRowSetAndOrder pins that the change
// moves metadata only: the same graph rows come back, in the same order, with
// every non-metadata field identical to the pre-#7247 page-read enrichment.
func TestLanguageMetadataAnchoredLookupKeepsRowSetAndOrder(t *testing.T) {
	t.Parallel()

	graphRows := anchoredGraphRows("Function", 50)
	anchored := newAnchoredStore(graphRows, "Function")
	before := &Handler{
		Neo4j:   &testutil.MockLanguageQueryGraphReader{Rows: anchoredGraphRows("Function", 50)},
		Content: &pageOnlyMetadataStore{page: anchored.page},
	}
	after := &Handler{
		Neo4j:   &testutil.MockLanguageQueryGraphReader{Rows: graphRows},
		Content: anchored,
	}

	beforeRows, _, err := before.queryByLanguage(t.Context(), "php", "Function", "", "", 50, anchoredGrant())
	if err != nil {
		t.Fatalf("before queryByLanguage() error = %v", err)
	}
	afterRows, _, err := after.queryByLanguage(t.Context(), "php", "Function", "", "", 50, anchoredGrant())
	if err != nil {
		t.Fatalf("after queryByLanguage() error = %v", err)
	}
	if got, want := len(afterRows), len(beforeRows); got != want {
		t.Fatalf("row count after = %d, before = %d", got, want)
	}
	for i := range beforeRows {
		if !reflect.DeepEqual(withoutEnrichment(afterRows[i]), withoutEnrichment(beforeRows[i])) {
			t.Fatalf("row %d differs outside metadata:\nbefore %#v\nafter  %#v", i, beforeRows[i], afterRows[i])
		}
	}
}

// withoutEnrichment keeps only the graph-sourced identity fields: the
// enrichment may change metadata and the semantic blocks derived from it
// (semantic_summary, semantic_profile, *_semantics), nothing else.
func withoutEnrichment(row map[string]any) map[string]any {
	out := make(map[string]any, 9)
	for _, key := range []string{
		"entity_id", "name", "labels", "file_path", "repo_id", "repo_name",
		"language", "start_line", "end_line",
	} {
		if value, ok := row[key]; ok {
			out[key] = value
		}
	}
	return out
}
