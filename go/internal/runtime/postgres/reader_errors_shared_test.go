// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestReaderSentinelsAreTheSharedDBSentinels pins #7523: the query layer
// classifies reader failures by db.ErrReaderStale and db.ErrReaderUnavailable
// without importing this package, so the values this package returns must be
// those exact errors, including through the private-failure wrapper that hides
// driver text.
func TestReaderSentinelsAreTheSharedDBSentinels(t *testing.T) {
	if ErrReaderStale != db.ErrReaderStale || ErrReaderUnavailable != db.ErrReaderUnavailable {
		t.Fatal("runtime reader sentinels are not the shared db sentinels")
	}
	stale := replayContextError(context.DeadlineExceeded)
	if !errors.Is(stale, db.ErrReaderStale) {
		t.Fatalf("replay timeout %v is not db.ErrReaderStale", stale)
	}
	poolWait := privateFailure(failureReaderBorrow, errors.Join(ErrReaderUnavailable, context.DeadlineExceeded))
	if !errors.Is(poolWait, db.ErrReaderUnavailable) {
		t.Fatalf("pool-wait failure %v is not db.ErrReaderUnavailable", poolWait)
	}
}
