// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

// TestCrossRepoDeadCodeConsumerEvidenceBindsTheGrantInTheShippedSQL moved out
// of codequery/auth_scoped_code_dead_code_cross_repo_grant_test.go at the
// #6060 CodeHandler move: it drives the real root *ContentReader over a
// recording SQL fake, which cannot move to codequery without recreating
// ContentReader there.
func TestCrossRepoDeadCodeConsumerEvidenceBindsTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	t.Run("scoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: crossRepoDeadCodeEvidenceColumns()},
			{columns: []string{"entity_id", "hidden_count"}},
		})
		reader := NewContentReader(db)
		if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			code.CrossRepoDeadCodeConsumerReads{
				PageRepositoryIDs: []string{codeGrantConsumerRepo},
				SignalGrant:       []string{codeGrantConsumerRepo},
			},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(recorder.queries) != 2 {
			t.Fatalf("query count = %d, want 2 (grant-bound evidence page plus ungranted-consumer probe)", len(recorder.queries))
		}
		want := "AND row.repository_id = ANY($3)"
		if !strings.Contains(recorder.queries[0], want) {
			t.Fatalf("consumer-evidence SQL is missing %q, so the LIMIT is still drawn from every tenant's rows:\n%s", want, recorder.queries[0])
		}
		if recorder.queries[1] != deadcode.CrossRepoDeadCodeUngrantedConsumerProbeQuery {
			t.Fatalf("second statement is not the ungranted-consumer probe:\n%s", recorder.queries[1])
		}
		bound := fmt.Sprintf("%s", recorder.args[0][2])
		if !strings.Contains(bound, codeGrantConsumerRepo) {
			t.Fatalf("grant argument = %q, want the encoded Postgres array carrying %q", bound, codeGrantConsumerRepo)
		}
	})

	t.Run("unscoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: crossRepoDeadCodeEvidenceColumns()},
		})
		reader := NewContentReader(db)
		if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			code.CrossRepoDeadCodeConsumerReads{},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(recorder.queries) != 1 {
			t.Fatalf("query count = %d, want 1 -- an unscoped caller must not pay for the probe", len(recorder.queries))
		}
		if strings.Contains(recorder.queries[0], "ANY(") {
			t.Fatalf("unscoped consumer-evidence SQL gained a grant clause:\n%s", recorder.queries[0])
		}
	})
}

// crossRepoDeadCodeGrantBoundPageGolden is the grant-bound evidence page for two
// entities and one granted repository, written out in full. It is the
// statement this route shipped before #7249, kept byte for byte: #7249 moved
// only the unscoped page to the per-entity lateral, because under a grant
// neither shape keeps the read bounded and the lateral costs more buffers.
const crossRepoDeadCodeGrantBoundPageGolden = `
SELECT row.entity_id,
       row.repository_id,
       '' AS consumer_repo_name,
       row.root_entity_id,
       row.depth,
       row.state,
       row.confidence,
       row.min_resolution_method,
       row.evidence,
       row.root_kinds,
       row.generation_id,
       generation.status AS generation_status,
       row.observed_at,
       row.updated_at
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.repository_id <> $1
  AND row.entity_id IN ($2, $3)
  AND row.depth > 0
  AND row.repository_id = ANY($4)
ORDER BY row.entity_id ASC, row.confidence DESC, row.depth ASC,
         row.repository_id ASC, row.root_entity_id ASC,
         row.scope_id ASC, row.generation_id ASC
LIMIT 1001
`

// TestCrossRepoDeadCodeGrantBoundPageKeepsTheShippedStatement pins the
// grant-bound evidence page to the statement that shipped before #7249. A change
// that routes grant-bound reads through the lateral -- or edits this statement
// at all -- fails here and has to bring its own proof for the grant-bound read:
// the lateral was measured reading more buffers than this statement under every
// grant size tried (docs/internal/evidence/7249-dead-code-reachability.md).
func TestCrossRepoDeadCodeGrantBoundPageKeepsTheShippedStatement(t *testing.T) {
	t.Parallel()

	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery(
		"repo-producer", []string{"entity-1", "entity-2"}, []string{"repo-a"},
	)
	if query != crossRepoDeadCodeGrantBoundPageGolden {
		t.Fatalf("the grant-bound evidence page changed:\n%s\nwant:\n%s", query, crossRepoDeadCodeGrantBoundPageGolden)
	}
	if got, want := len(args), 4; got != want {
		t.Fatalf("len(args) = %d, want %d (producer, two entities, grant array)", got, want)
	}
	if got := fmt.Sprintf("%v %v %v", args[0], args[1], args[2]); got != "repo-producer entity-1 entity-2" {
		t.Fatalf("args = %s, want the producer then each entity in page order", got)
	}
	if bound := fmt.Sprintf("%s", args[3]); !strings.Contains(bound, "repo-a") {
		t.Fatalf("grant argument = %q, want the encoded array carrying repo-a", bound)
	}
}

// TestCrossRepoDeadCodeUnscopedPageIsTheLateral pins the other half: a read
// with no consumer list takes the per-entity lateral, whose text does not vary
// with the page, binding the producer and the page as one array.
func TestCrossRepoDeadCodeUnscopedPageIsTheLateral(t *testing.T) {
	t.Parallel()

	for _, page := range [][]string{{"entity-1"}, {"entity-1", "entity-2", "entity-3"}} {
		query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery("repo-producer", page, nil)
		if query != crossRepoDeadCodeConsumerEvidenceLateralQuery {
			t.Fatalf("an unscoped page of %d entities did not take the lateral:\n%s", len(page), query)
		}
		if got, want := len(args), 2; got != want {
			t.Fatalf("len(args) = %d, want %d (producer, entity array)", got, want)
		}
		if bound := fmt.Sprintf("%s", args[1]); !strings.Contains(bound, page[len(page)-1]) {
			t.Fatalf("entity argument = %q, want the encoded array carrying the page", bound)
		}
	}
}
