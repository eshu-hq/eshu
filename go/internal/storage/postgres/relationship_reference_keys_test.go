// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRelationshipReferenceCandidateKeyRowsTokenizeAcceptedPayload(t *testing.T) {
	t.Parallel()

	rows := relationshipReferenceCandidateKeyRows([]facts.Envelope{{
		FactID:       "fact-1",
		ScopeID:      "git-repository-scope:github.com/acme/app",
		GenerationID: "gen-1",
		FactKind:     "content",
		ObservedAt:   time.Unix(1, 0).UTC(),
		Payload: map[string]any{
			"repo_id":       "github.com/acme/app",
			"artifact_type": "github_actions_workflow",
			"relative_path": ".github/workflows/deploy.yaml",
			"content":       "uses: github.com/acme/app-config/.github/workflows/deploy.yaml@main",
		},
	}})
	if len(rows) != 1 {
		t.Fatalf("relationshipReferenceCandidateKeyRows() returned %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.FactID != "fact-1" || row.ScopeID != "git-repository-scope:github.com/acme/app" || row.GenerationID != "gen-1" {
		t.Fatalf("row identity = %#v", row)
	}
	if row.SourceRepoID != "github.com/acme/app" {
		t.Fatalf("SourceRepoID = %q, want github.com/acme/app", row.SourceRepoID)
	}
	if !strings.Contains(row.ReferenceKey, "|github.com|acme|app-config|") {
		t.Fatalf("ReferenceKey %q missing target repo token stream", row.ReferenceKey)
	}
	if !strings.Contains(row.ReferenceKey, "|github.com|acme|app|") {
		t.Fatalf("ReferenceKey %q missing source repo token stream for SQL self-exclusion", row.ReferenceKey)
	}
}

func TestRelationshipReferenceCandidateKeyRowsSkipTombstonesAndUnsupportedKinds(t *testing.T) {
	t.Parallel()

	rows := relationshipReferenceCandidateKeyRows([]facts.Envelope{
		{
			FactID:       "tombstone",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "content",
			IsTombstone:  true,
			Payload:      map[string]any{"repo_id": "repo-a", "content": "repo-b"},
		},
		{
			FactID:       "repository",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "repository",
			Payload:      map[string]any{"repo_id": "repo-a", "name": "repo-b"},
		},
	})
	if len(rows) != 0 {
		t.Fatalf("relationshipReferenceCandidateKeyRows() = %#v, want no rows", rows)
	}
}

func TestRefreshRelationshipReferenceCandidateKeysDeletesAcceptedFactIDsBeforeInsert(t *testing.T) {
	t.Parallel()

	db := &relationshipReferenceExecRecorder{}
	err := refreshRelationshipReferenceCandidateKeys(context.Background(), db, []facts.Envelope{
		{
			FactID:       "fact-live",
			ScopeID:      "git-repository-scope:github.com/acme/app",
			GenerationID: "gen-1",
			FactKind:     "content",
			ObservedAt:   time.Unix(1, 0).UTC(),
			Payload: map[string]any{
				"repo_id": "github.com/acme/app",
				"content": "uses github.com/acme/platform",
			},
		},
		{
			FactID:       "fact-tombstone",
			ScopeID:      "git-repository-scope:github.com/acme/app",
			GenerationID: "gen-1",
			FactKind:     "content",
			IsTombstone:  true,
			Payload:      map[string]any{"repo_id": "github.com/acme/app"},
		},
		{
			FactID:       "fact-repository",
			ScopeID:      "git-repository-scope:github.com/acme/app",
			GenerationID: "gen-1",
			FactKind:     "repository",
			Payload:      map[string]any{"repo_id": "github.com/acme/app"},
		},
	})
	if err != nil {
		t.Fatalf("refreshRelationshipReferenceCandidateKeys() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM relationship_reference_candidate_keys") {
		t.Fatalf("first exec query = %q, want delete", db.execs[0].query)
	}
	deleted, ok := db.execs[0].args[0].(pq.StringArray)
	if !ok {
		t.Fatalf("delete arg type = %T, want pq.StringArray", db.execs[0].args[0])
	}
	wantDeleted := []string{"fact-live", "fact-tombstone", "fact-repository"}
	for i, want := range wantDeleted {
		if i >= len(deleted) || deleted[i] != want {
			t.Fatalf("deleted fact ids = %v, want %v", []string(deleted), wantDeleted)
		}
	}
	if !strings.Contains(db.execs[1].query, "INSERT INTO relationship_reference_candidate_keys") {
		t.Fatalf("second exec query = %q, want insert", db.execs[1].query)
	}
	if got, want := len(db.execs[1].args), columnsPerRelationshipReferenceCandidateKeyRow; got != want {
		t.Fatalf("insert args = %d, want one candidate row (%d args)", got, want)
	}
	if got, want := db.execs[1].args[0], "fact-live"; got != want {
		t.Fatalf("insert fact_id = %v, want %q", got, want)
	}
	if got, want := db.execs[1].args[3], "github.com/acme/app"; got != want {
		t.Fatalf("insert source_repo_id = %v, want %q", got, want)
	}
	referenceKey, ok := db.execs[1].args[4].(string)
	if !ok {
		t.Fatalf("insert reference key type = %T, want string", db.execs[1].args[4])
	}
	if !strings.Contains(referenceKey, "|github.com|acme|platform|") {
		t.Fatalf("insert reference key %q missing target repo token stream", referenceKey)
	}
}

func TestRefreshRelationshipReferenceCandidateKeysDeletesOnlyWhenNoCandidatesRemain(t *testing.T) {
	t.Parallel()

	db := &relationshipReferenceExecRecorder{}
	err := refreshRelationshipReferenceCandidateKeys(context.Background(), db, []facts.Envelope{
		{
			FactID:       "fact-tombstone",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "content",
			IsTombstone:  true,
			Payload:      map[string]any{"repo_id": "repo-a"},
		},
		{
			FactID:       "fact-retyped",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "repository",
			Payload:      map[string]any{"repo_id": "repo-a"},
		},
	})
	if err != nil {
		t.Fatalf("refreshRelationshipReferenceCandidateKeys() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM relationship_reference_candidate_keys") {
		t.Fatalf("exec query = %q, want delete", db.execs[0].query)
	}
	deleted := db.execs[0].args[0].(pq.StringArray)
	wantDeleted := []string{"fact-tombstone", "fact-retyped"}
	for i, want := range wantDeleted {
		if i >= len(deleted) || deleted[i] != want {
			t.Fatalf("deleted fact ids = %v, want %v", []string(deleted), wantDeleted)
		}
	}
}

type relationshipReferenceExecRecorder struct {
	execs []relationshipReferenceExecCall
}

type relationshipReferenceExecCall struct {
	query string
	args  []any
}

func (r *relationshipReferenceExecRecorder) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.execs = append(r.execs, relationshipReferenceExecCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	return nil, nil
}

func (r *relationshipReferenceExecRecorder) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("relationshipReferenceExecRecorder.QueryContext must not be called")
}
