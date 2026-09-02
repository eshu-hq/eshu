// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Test doubles for the cross-repo resolution interfaces. The family's own
// copies live in internal/reducer/crossrepo (issue #6061); Go test files
// cannot share unexported symbols across a package boundary, so the root
// tests that exercise CrossRepoRelationshipHandler through the aliases in
// cross_repo_compat.go keep their own copies here.

type fakeEvidenceFactLoader struct {
	facts []relationships.EvidenceFact
	err   error
	calls int
}

func (f *fakeEvidenceFactLoader) ListEvidenceFacts(_ context.Context, _ string) ([]relationships.EvidenceFact, error) {
	f.calls++
	return f.facts, f.err
}

type recordingRepoDependencyIntentWriter struct {
	rows [][]sharedintent.Row
}

func (r *recordingRepoDependencyIntentWriter) UpsertIntents(_ context.Context, rows []sharedintent.Row) error {
	r.rows = append(r.rows, append([]sharedintent.Row(nil), rows...))
	return nil
}

// stringValue reads a payload value as a string, returning "" for any other
// dynamic type. The crossrepo package keeps its own copy for the same reason
// the doubles above are duplicated.
func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
