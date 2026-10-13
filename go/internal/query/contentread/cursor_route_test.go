// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// A cursor that names an entity (start_line or entity_id) sent to the file
// route is refused: the file walk binds only repo_id and relative_path, so it
// would silently resume from the whole file instead of the named entity, and
// the client would believe the cursor was honored (#7009 ruling, 2.3).
func TestSearchFilesRefusesAnEntityCursor(t *testing.T) {
	t.Parallel()

	for name, cursor := range map[string]string{
		"start_line": `{"repo_id":"r9","relative_path":"z.go","start_line":12}`,
		"entity_id":  `{"repo_id":"r9","relative_path":"z.go","entity_id":"entity-9"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &unscopedSearchStore{}
			h := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}
			rec, _ := postFileSearch(t, h, context.Background(), `{"query":"needle","cursor":`+cursor+`}`)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), errFileCursorNamesEntity.Error()) {
				t.Fatalf("body = %s, want %q", rec.Body.String(), errFileCursorNamesEntity.Error())
			}
			if store.calls != 0 {
				t.Fatalf("unscoped seam called %d times for a refused cursor", store.calls)
			}
		})
	}
}

// A cursor without entity_id sent to the entity route is refused: a
// file-search cursor names no entity, and resuming after the file key alone
// would skip the rest of that file's entities (#7009 ruling, 2.3).
func TestSearchEntitiesRefusesAFileCursor(t *testing.T) {
	t.Parallel()

	store := &unscopedEntitySearchStore{}
	h := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}
	rec, _ := postEntitySearch(t, h, context.Background(),
		`{"query":"render","cursor":{"repo_id":"r9","relative_path":"z.go"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errEntityCursorNeedsEntityID.Error()) {
		t.Fatalf("body = %s, want %q", rec.Body.String(), errEntityCursorNeedsEntityID.Error())
	}
	if store.calls != 0 {
		t.Fatalf("unscoped seam called %d times for a refused cursor", store.calls)
	}
}
