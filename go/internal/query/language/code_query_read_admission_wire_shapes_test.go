// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
)

// codeLanguageQueryAdmissionCannedStore is the pin-satisfying double
// behind the wire-shape proof: it implements exactly the four pinned
// language reads over fixed canned rows and is compile-asserted against
// the pin, so the proof below runs through the admission shape, not the
// wider staying ports.
type codeLanguageQueryAdmissionCannedStore struct {
	entity querycontract.EntityContent
}

func codeLanguageQueryAdmissionCannedEntity() querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityID:     "entity-wire-1",
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		EntityType:   "Function",
		EntityName:   "handleWire",
		StartLine:    3,
		EndLine:      9,
		Language:     "go",
		SourceCache:  "func handleWire() {}",
		Metadata:     map[string]any{"wire": "entity"},
	}
}

func (s *codeLanguageQueryAdmissionCannedStore) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"wire": "graph-row", "entity_id": "entity-wire-1", "repo_id": "repo-wire"}}, nil
}

func (s *codeLanguageQueryAdmissionCannedStore) SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error) {
	return []querycontract.EntityContent{s.entity}, nil
}

func (s *codeLanguageQueryAdmissionCannedStore) SearchEntitiesByLanguageAndTypeForAccess(_ context.Context, search taxonomy.LanguageEntitySearch) ([]querycontract.EntityContent, error) {
	if search.Query != "handleWire" {
		return nil, nil
	}
	return []querycontract.EntityContent{s.entity}, nil
}

func (s *codeLanguageQueryAdmissionCannedStore) ListEntityMetadataByIDsForAccess(_ context.Context, lookup taxonomy.LanguageEntityIDLookup) ([]querycontract.EntityContent, error) {
	if len(lookup.EntityIDs) == 0 {
		return nil, nil
	}
	return []querycontract.EntityContent{s.entity}, nil
}

var _ CodeLanguageQueryReadAdmission = (*codeLanguageQueryAdmissionCannedStore)(nil)

// marshalCodeLanguageQueryAdmissionWire projects one value through the
// same encoding the handlers use (no HTML escaping), so the proof
// compares wire bytes the way a satellite consumer would read them, not
// Go structs that could drift from the wire.
func marshalCodeLanguageQueryAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestCodeLanguageQueryReadAdmissionWireShapesAreSatelliteConsumable
// proves a satellite can consume the pin using only the pin plus the
// querycontract and taxonomy shapes: all four pinned reads answer
// through a CodeLanguageQueryReadAdmission-typed value, the grant-bound
// search and the id-anchored lookup carry the caller grant alongside the
// filters end to end, and an unmatching search answers with zero rows
// and no error. A fail-closed nil entity marshals to null, so the
// satellite path has no hidden non-nullable expectation. The per-repo
// fallback loops and the page-read fallback stay handler behavior OUT.
func TestCodeLanguageQueryReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin CodeLanguageQueryReadAdmission = &codeLanguageQueryAdmissionCannedStore{
		entity: codeLanguageQueryAdmissionCannedEntity(),
	}

	rows, err := pin.Run(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || len(rows) != 1 || rows[0]["entity_id"] != "entity-wire-1" {
		t.Fatalf("Run() = %#v, %v, want the one canned graph row", rows, err)
	}
	if wire := marshalCodeLanguageQueryAdmissionWire(t, rows); !strings.Contains(wire, `"entity_id":"entity-wire-1"`) {
		t.Errorf("graph wire bytes = %s, want the canned entity id", wire)
	}

	unscoped, err := pin.SearchEntitiesByLanguageAndType(ctx, "repo-wire", "go", "Function", "handleWire", 10)
	if err != nil || len(unscoped) != 1 || unscoped[0].EntityID != "entity-wire-1" {
		t.Fatalf("SearchEntitiesByLanguageAndType() = %#v, %v, want the canned entity", unscoped, err)
	}

	grantSearch := taxonomy.LanguageEntitySearch{
		Language:             "go",
		EntityType:           "Function",
		Query:                "handleWire",
		Limit:                10,
		AllowedRepositoryIDs: []string{"repo-wire"},
	}
	grantPage, err := pin.SearchEntitiesByLanguageAndTypeForAccess(ctx, grantSearch)
	if err != nil || len(grantPage) != 1 || grantPage[0].EntityID != "entity-wire-1" {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess() = %#v, %v, want the canned entity for the granted search", grantPage, err)
	}
	if wire := marshalCodeLanguageQueryAdmissionWire(t, grantSearch); !strings.Contains(wire, `"AllowedRepositoryIDs":["repo-wire"]`) {
		t.Errorf("grant search wire bytes = %s, want the caller grant carried alongside the filters", wire)
	}

	anchored, err := pin.ListEntityMetadataByIDsForAccess(ctx, taxonomy.LanguageEntityIDLookup{
		EntityIDs:            []string{"entity-wire-1"},
		AllowedRepositoryIDs: []string{"repo-wire"},
	})
	if err != nil || len(anchored) != 1 || anchored[0].RepoID != "repo-wire" {
		t.Fatalf("ListEntityMetadataByIDsForAccess() = %#v, %v, want the canned metadata row", anchored, err)
	}
	if wire := marshalCodeLanguageQueryAdmissionWire(t, anchored[0]); !strings.Contains(wire, `"entity_id":"entity-wire-1"`) {
		t.Errorf("metadata wire bytes = %s, want the entity id key", wire)
	}

	emptyLookup, err := pin.ListEntityMetadataByIDsForAccess(ctx, taxonomy.LanguageEntityIDLookup{})
	if err != nil || len(emptyLookup) != 0 {
		t.Fatalf("ListEntityMetadataByIDsForAccess() empty = %#v, %v, want zero rows and no error", emptyLookup, err)
	}

	emptySearch := grantSearch
	emptySearch.Query = "nothing-matches"
	empty, err := pin.SearchEntitiesByLanguageAndTypeForAccess(ctx, emptySearch)
	if err != nil || len(empty) != 0 {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess() unmatching = %#v, %v, want zero rows and no error", empty, err)
	}
	if wire := marshalCodeLanguageQueryAdmissionWire(t, (*querycontract.EntityContent)(nil)); wire != "null" {
		t.Errorf("nil entity wire bytes = %s, want null (fail-closed emptiness has no hidden shape)", wire)
	}
}
