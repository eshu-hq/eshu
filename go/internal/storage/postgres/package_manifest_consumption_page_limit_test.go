// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// limitCapturingQueryer records the arguments of the page query and stops the
// loader with a sentinel error so no rows are read.
type limitCapturingQueryer struct {
	args []any
}

var errPageLimitCaptured = errors.New("page limit captured")

func (q *limitCapturingQueryer) QueryContext(_ context.Context, _ string, args ...any) (db.Rows, error) {
	q.args = args
	return nil, errPageLimitCaptured
}

// TestPackageManifestConsumptionPageLimitEqualsRebuilderPageSize pins the
// coupling the paged rebuilder depends on: it ends a scope on the first page
// shorter than PageSize, so the LIMIT the production page loader binds must be
// exactly the PageSize the production rebuilder is built with. A drift in
// either direction would end a scope early and clear its dirty fence while
// facts remain unprocessed (#7663 review, P3-1).
func TestPackageManifestConsumptionPageLimitEqualsRebuilderPageSize(t *testing.T) {
	rebuilder := PackageManifestConsumptionKeyBackfill{}.rebuilder(nil)
	if rebuilder.PageSize <= 0 {
		t.Fatalf("rebuilder PageSize = %d, want > 0", rebuilder.PageSize)
	}
	queryer := &limitCapturingQueryer{}
	_, err := rebuilder.LoadPage(context.Background(), queryer, "scope-a", "")
	if !errors.Is(err, errPageLimitCaptured) {
		t.Fatalf("LoadPage error = %v, want the capture sentinel", err)
	}
	if len(queryer.args) != 3 {
		t.Fatalf("page query bound %d arguments, want 3 (scope, cursor, limit)", len(queryer.args))
	}
	limit, ok := queryer.args[2].(int)
	if !ok {
		t.Fatalf("page LIMIT argument is %T, want int", queryer.args[2])
	}
	if limit != rebuilder.PageSize {
		t.Fatalf("page LIMIT = %d but rebuilder PageSize = %d; a short page would end the scope early", limit, rebuilder.PageSize)
	}
}
