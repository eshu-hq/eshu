// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// wholeScopeRefreshPayload builds the production payload shape of a whole-scope
// retract row for a FENCED repo-wide-retract domain -- inheritance, rationale,
// SQL relationships, shell exec. That is the per-repo refresh intent, and the
// refresh intent_type is the part that matters: since #6166 those four domains
// bind collectWholeScopeRefreshRepoIDs on their non-delta branch, so a row
// without it contributes nothing to the retract.
//
// Retract tests for those domains MUST build payloads through this helper
// rather than writing a bare map[string]any{"repo_id": ...}. A bare payload is
// an unmarked legacy per-edge row: no current emitter produces one (every
// per-edge intent is stamped retract_via_refresh at emission), and the narrowed
// dispatch deliberately excludes it. Before #6166 a whole family of retract
// tests used that shape and passed against it -- which meant that after the
// narrowing they would have kept passing while binding an EMPTY repo_ids list,
// asserting on a DELETE that deletes nothing under names like
// "RunsDeleteWhenProbeFindsRows". Pair this helper with assertBoundRepoIDs so a
// test proves WHICH repositories the statement bound, not merely that some
// statement ran.
func wholeScopeRefreshPayload(repoID string) map[string]any {
	return map[string]any{
		"repo_id":     repoID,
		"intent_type": reducer.RepoRefreshIntentType,
		"action":      "refresh",
	}
}

// wholeScopeRefreshRetractRow is wholeScopeRefreshPayload wrapped in the intent
// row the retract dispatch receives.
func wholeScopeRefreshRetractRow(intentID, repoID string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     intentID,
		RepositoryID: repoID,
		Payload:      wholeScopeRefreshPayload(repoID),
	}
}

// assertBoundRepoIDs fails unless at least one recorded statement binds
// $repo_ids, and every statement that binds it binds exactly want.
//
// Asserting on the Cypher text alone cannot catch an empty binding: the
// statement is built and executed either way, so `strings.Contains(stmt.Cypher,
// "DELETE rel")` stays true while the DELETE matches nothing. The bound
// parameter is the only thing that distinguishes a retract that ran from one
// that ran over an empty set.
func assertBoundRepoIDs(t *testing.T, stmts []Statement, want []string) {
	t.Helper()
	sawBinding := false
	for _, stmt := range stmts {
		raw, ok := stmt.Parameters["repo_ids"]
		if !ok {
			continue
		}
		sawBinding = true
		got, ok := raw.([]string)
		if !ok {
			t.Fatalf("statement bound repo_ids of type %T, want []string (cypher %q)", raw, stmt.Cypher)
		}
		if len(got) != len(want) {
			t.Fatalf("bound repo_ids = %v, want %v (cypher %q)", got, want, stmt.Cypher)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("bound repo_ids = %v, want %v (cypher %q)", got, want, stmt.Cypher)
			}
		}
	}
	if !sawBinding {
		t.Fatalf("no executed statement bound $repo_ids at all; wanted %v", want)
	}
}
