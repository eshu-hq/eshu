// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingCatalogReader struct {
	query string
	err   error
}

func (r *rejectingCatalogReader) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	r.query = query
	return nil, r.err
}

func TestAdvisoryCatalogQueryOnlyReadPort(t *testing.T) {
	want := errors.New("reader unavailable")
	reader := &rejectingCatalogReader{err: want}
	store := NewPostgresCatalogStoreWithReadStore(reader)
	_, err := store.ListAdvisoryCatalog(t.Context(), CatalogFilter{Limit: 1})
	if !errors.Is(err, want) || reader.query != ListCatalogQuery {
		t.Fatalf("error %v, query %q; want reader error and catalog SQL", err, reader.query)
	}
}
