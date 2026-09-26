// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestContentReaderInvestigateCodeTopicCappedContractLive exercises the
// production SQL against a disposable PostgreSQL database. It proves that the
// static entity branches retain the bounded topic-query contract for a capped
// pool, source-cache-only hit, overlapping terms, corpus grant, language
// filter, and stable pagination.
//
// Run against a disposable PostgreSQL database:
//
//	ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN=postgres://postgres@127.0.0.1:55433/postgres?sslmode=disable \
//	ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
//	go test ./internal/query -run TestContentReaderInvestigateCodeTopicCappedContractLive -count=1
func TestContentReaderInvestigateCodeTopicCappedContractLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	if err := storagepostgres.EnsureContentSearchIndexes(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("EnsureContentSearchIndexes(): %v", err)
	}
	seedCodeTopicLiveProofCorpus(t, ctx, database)

	reader := NewContentReader(database)
	cappedRows, err := reader.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{
		Terms:                codeTopicSixteenTerms(),
		AllowedRepositoryIDs: []string{"repo-granted"},
		Language:             "go",
		Limit:                250,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic(capped): %v", err)
	}
	if got, want := len(cappedRows), 250; got != want {
		t.Fatalf("capped rows = %d, want %d", got, want)
	}
	if !cappedRows[0].PoolTruncated {
		t.Fatal("capped pool marker = false, want true at the 16-term per-branch cap")
	}
	if cappedRows[0].EntityID != "capped-001" || cappedRows[len(cappedRows)-1].EntityID != "capped-250" {
		t.Fatalf("capped page boundaries = (%q, %q), want (capped-001, capped-250)", cappedRows[0].EntityID, cappedRows[len(cappedRows)-1].EntityID)
	}
	for _, row := range cappedRows {
		if row.RepoID != "repo-granted" || row.Language != "go" || row.EntityID == "capped-251" {
			t.Fatalf("capped row = %+v, want only selected granted go entities", row)
		}
	}

	sourceRows, err := reader.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{
		RepoID: "repo-source", Terms: []string{"source-only"}, Limit: 10,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic(source cache): %v", err)
	}
	if got, want := len(sourceRows), 1; got != want || sourceRows[0].EntityID != "source-only-entity" {
		t.Fatalf("source-cache rows = %+v, want source-only-entity", sourceRows)
	}
	for _, fileCase := range []struct {
		term, path string
	}{
		{term: "content-only", path: "ordinary-content.go"},
		{term: "path-only", path: "path-only.go"},
	} {
		fileRows, err := reader.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{
			RepoID: "repo-files", Terms: []string{fileCase.term}, Limit: 10,
		})
		if err != nil {
			t.Fatalf("InvestigateCodeTopic(file %s): %v", fileCase.term, err)
		}
		if got, want := len(fileRows), 1; got != want || fileRows[0].SourceKind != "file" || fileRows[0].RelativePath != fileCase.path {
			t.Fatalf("file %s rows = %+v, want %s-only file evidence", fileCase.term, fileRows, fileCase.path)
		}
	}

	overlapRows, err := reader.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{
		RepoID: "repo-granted", Terms: []string{"alpha", "beta"}, Limit: 10,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic(overlap): %v", err)
	}
	if got, want := len(overlapRows), 1; got != want || overlapRows[0].EntityID != "overlap-entity" || overlapRows[0].Score != 2 {
		t.Fatalf("overlapping rows = %+v, want one two-term overlap entity", overlapRows)
	}

	tieRows, err := reader.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{
		RepoID: "repo-ties", Terms: []string{"tie"}, Limit: 1, Offset: 1,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic(tie page): %v", err)
	}
	if got, want := len(tieRows), 1; got != want || tieRows[0].EntityID != "tie-b" {
		t.Fatalf("tie page = %+v, want stable second entity tie-b", tieRows)
	}
}

func codeTopicSixteenTerms() []string {
	return []string{
		"topic", "absent-01", "absent-02", "absent-03", "absent-04", "absent-05",
		"absent-06", "absent-07", "absent-08", "absent-09", "absent-10", "absent-11",
		"absent-12", "absent-13", "absent-14", "absent-15",
	}
}

func seedCodeTopicLiveProofCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name, start_line,
  end_line, language, source_cache, metadata, indexed_at
)
SELECT 'capped-' || lpad(n::text, 3, '0'), 'repo-granted',
       'capped/' || lpad(n::text, 3, '0') || '.go', 'Function',
       'topic-' || n, 1, 2, 'go', '', '{}', clock_timestamp()
FROM generate_series(1, 251) AS n;
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name, start_line,
  end_line, language, source_cache, metadata, indexed_at
) VALUES
  ('000-denied-topic', 'repo-denied', 'denied.go', 'Function', 'topic-denied', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('000-language-topic', 'repo-granted', 'language.js', 'Function', 'topic-js', 1, 2, 'javascript', '', '{}', clock_timestamp()),
  ('source-only-entity', 'repo-source', 'source.go', 'Function', 'ordinary-name', 1, 2, 'go', 'source-only payload', '{}', clock_timestamp()),
  ('overlap-entity', 'repo-granted', 'overlap.go', 'Function', 'alpha-beta', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('tie-a', 'repo-ties', 'tie.go', 'Function', 'tie', 1, 2, 'go', '', '{}', clock_timestamp()),
  ('tie-b', 'repo-ties', 'tie.go', 'Function', 'tie', 1, 2, 'go', '', '{}', clock_timestamp());
INSERT INTO content_files (
  repo_id, relative_path, content, content_hash, line_count, language, indexed_at
) VALUES
  ('repo-granted', 'topic-file.go', 'topic file body', 'topic-file-hash', 1, 'go', clock_timestamp()),
  ('repo-files', 'ordinary-content.go', 'content-only', 'content-only-hash', 1, 'go', clock_timestamp()),
  ('repo-files', 'path-only.go', 'ordinary body', 'path-only-hash', 1, 'go', clock_timestamp());
`)
	if err != nil {
		t.Fatalf("seed code-topic proof corpus: %v", err)
	}
}
