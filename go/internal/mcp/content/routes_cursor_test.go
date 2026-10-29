// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contenttools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

func body(t *testing.T, request routecontract.Request) map[string]any {
	t.Helper()
	typed, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("request body = %T, want map[string]any", request.Body)
	}
	return typed
}

// The resume cursor of a partial unscoped search is forwarded verbatim to the
// file-search route and the entity-search cursor to the entity-search route.
// A cursor on a scoped search is refused by the handler, never silently
// ignored.
func TestSearchFileContentForwardsResumeCursor(t *testing.T) {
	t.Parallel()

	cursor := map[string]any{"repo_id": "r9", "relative_path": "z.go"}
	args := routecontract.Arguments{"pattern": "render", "cursor": cursor}

	file, _ := Route("search_file_content", args)
	if got := body(t, file)["cursor"]; !reflect.DeepEqual(got, cursor) {
		t.Fatalf("search_file_content body cursor = %#v, want %#v", got, cursor)
	}
	entityCursor := map[string]any{"repo_id": "r9", "relative_path": "z.go", "start_line": 12, "entity_id": "entity-9"}
	entity, _ := Route("search_entity_content", routecontract.Arguments{"pattern": "render", "cursor": entityCursor})
	if got := body(t, entity)["cursor"]; !reflect.DeepEqual(got, entityCursor) {
		t.Fatalf("search_entity_content body cursor = %#v, want %#v", got, entityCursor)
	}
	plain, _ := Route("search_file_content", routecontract.Arguments{"pattern": "render"})
	plain1 := body(t, plain)
	if _, present := plain1["cursor"]; present {
		t.Fatalf("a call without a cursor must not send one: %#v", plain.Body)
	}
	plainEntity, _ := Route("search_entity_content", routecontract.Arguments{"pattern": "render"})
	if _, present := body(t, plainEntity)["cursor"]; present {
		t.Fatalf("an entity call without a cursor must not send one: %#v", plainEntity.Body)
	}
}

func TestSearchFileContentForwardsBudgetOnlyWhenProvided(t *testing.T) {
	t.Parallel()
	file, _ := Route("search_file_content", routecontract.Arguments{"pattern": "render", "budget_ms": 1200})
	if got := body(t, file)["budget_ms"]; got != 1200 {
		t.Fatalf("file budget_ms = %#v, want 1200", got)
	}
	plain, _ := Route("search_file_content", routecontract.Arguments{"pattern": "render"})
	if _, present := body(t, plain)["budget_ms"]; present {
		t.Fatal("omitted budget_ms was forwarded")
	}
	entity, _ := Route("search_entity_content", routecontract.Arguments{"pattern": "render", "budget_ms": 1200})
	if _, present := body(t, entity)["budget_ms"]; present {
		t.Fatal("file-only budget_ms was forwarded to entity search")
	}
}

// TestSearchEntityContentForwardsRankCursor pins that the #7746 rank cursor
// reaches the entity route, and that a call without one sends no key.
func TestSearchEntityContentForwardsRankCursor(t *testing.T) {
	t.Parallel()
	entity, _ := Route("search_entity_content", routecontract.Arguments{"pattern": "render", "repo_ids": []any{"r1"}, "rank_cursor": "start"})
	if got := body(t, entity)["rank_cursor"]; got != "start" {
		t.Fatalf("search_entity_content body rank_cursor = %#v, want start", got)
	}
	plain, _ := Route("search_entity_content", routecontract.Arguments{"pattern": "render"})
	if _, present := body(t, plain)["rank_cursor"]; present {
		t.Fatalf("a call without rank_cursor must not send one: %#v", plain.Body)
	}
	file, _ := Route("search_file_content", routecontract.Arguments{"pattern": "render", "rank_cursor": "start"})
	if got := body(t, file)["rank_cursor"]; got != "start" {
		t.Fatalf("search_file_content body rank_cursor = %#v, want start so the route refuses it", got)
	}
}
