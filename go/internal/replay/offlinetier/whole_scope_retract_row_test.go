// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import "github.com/eshu-hq/eshu/go/internal/reducer"

// wholeScopeRefreshRetractPayload builds the production payload shape of a
// whole-scope retract row for a FENCED repo-wide-retract domain: inheritance,
// rationale, SQL relationships, shell exec. That row is the per-repo refresh
// intent the domain's materializer emits -- inheritance.BuildRefreshIntents,
// buildRationaleRefreshIntents, sqlrelationship.BuildRefreshIntents and
// buildShellExecRefreshIntents, each in go/internal/reducer/*_intents.go, all
// of which stamp intent_type unconditionally -- and the refresh intent_type is
// the part the retract dispatch keys on.
//
// Live-tier retract proofs for those four domains MUST build their whole-repo
// row through this helper rather than leaving Payload nil or writing a bare
// map[string]any{"repo_id": ...}. Since #6166 the non-delta branch of each of
// the four binds collectWholeScopeRefreshRepoIDs
// (go/internal/storage/cypher/edge_writer_retract_scope.go), which requires
// that intent_type. A nil or bare payload is an unmarked legacy per-edge row --
// a shape no emitter can produce, because every per-edge intent is stamped
// retract_via_refresh at emission -- so the dispatch returns before it builds a
// statement. The proof then runs ZERO statements against the live backend, the
// edges it just wrote are still there, and every "the edges are gone" assertion
// fails. The gate does not go quietly green; it breaks, and it breaks a long
// way from the payload that caused it.
//
// It mirrors wholeScopeRefreshPayload in package cypher. The two cannot be
// shared: that one lives in an in-package test file of the writer's own
// package, and the "refresh" action string is unexported in reducer.
func wholeScopeRefreshRetractPayload(repoID string) map[string]any {
	return map[string]any{
		"repo_id":     repoID,
		"intent_type": reducer.RepoRefreshIntentType,
		"action":      "refresh",
	}
}
