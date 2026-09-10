// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// fakeDeadCodeContentStore adapts querytestutil.FakeDeadCodeContentStore to
// the field names the codequery tests already use. Neither read is
// reimplemented here: both live in querytestutil, the single home for the
// double, so this adapter cannot drift from the fake the deadcode leaf
// family uses. A symbol declared in a _test.go file cannot be imported
// across a package boundary, which is why the deadcode leaf keeps its own
// twin of this adapter rather than importing this one.
type fakeDeadCodeContentStore struct {
	querytestutil.FakePortContentStore
	entities          map[string]deadcode.EntityContent
	incomingEntityIDs map[string]bool
}

// promoted converts this adapter into the shared double it delegates to.
func (f fakeDeadCodeContentStore) promotedDeadCode() querytestutil.FakeDeadCodeContentStore {
	return querytestutil.FakeDeadCodeContentStore{
		FakePortContentStore: f.FakePortContentStore,
		Entities:             f.entities,
		IncomingEntityIDs:    f.incomingEntityIDs,
	}
}

func (f fakeDeadCodeContentStore) GetEntityContent(ctx context.Context, entityID string) (*deadcode.EntityContent, error) {
	return f.promotedDeadCode().GetEntityContent(ctx, entityID)
}

func (f fakeDeadCodeContentStore) DeadCodeIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string) (map[string]deadcode.DeadCodeIncomingEdge, error) {
	return f.promotedDeadCode().DeadCodeIncomingEntityIDs(ctx, repoID, entityIDs)
}
