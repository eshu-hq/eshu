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

// The resume cursor of a partial unscoped file search is forwarded verbatim to
// the file-search route and to no other route: the entity search refuses a
// cursor, so forwarding it there would turn a typo into a 400.
func TestSearchFileContentForwardsResumeCursor(t *testing.T) {
	t.Parallel()

	cursor := map[string]any{"repo_id": "r9", "relative_path": "z.go"}
	args := routecontract.Arguments{"pattern": "render", "cursor": cursor}

	file, _ := Route("search_file_content", args)
	if got := body(t, file)["cursor"]; !reflect.DeepEqual(got, cursor) {
		t.Fatalf("search_file_content body cursor = %#v, want %#v", got, cursor)
	}
	entity, _ := Route("search_entity_content", args)
	if _, present := body(t, entity)["cursor"]; present {
		t.Fatalf("search_entity_content body carries a cursor: %#v", entity.Body)
	}
	plain, _ := Route("search_file_content", routecontract.Arguments{"pattern": "render"})
	plain1 := body(t, plain)
	if _, present := plain1["cursor"]; present {
		t.Fatalf("a call without a cursor must not send one: %#v", plain.Body)
	}
}
