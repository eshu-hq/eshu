// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/url"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	runtimepostgres "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

type codeTopicGuardedReadObserver struct{ borrows atomic.Int64 }

func (o *codeTopicGuardedReadObserver) Observe(role string, stage runtimepostgres.Stage, outcome runtimepostgres.Outcome, _ time.Duration) {
	if role == "reader" && stage == runtimepostgres.StageReaderBorrow && outcome == runtimepostgres.OutcomeOK {
		o.borrows.Add(1)
	}
}

// TestInvestigateCodeTopicGuardedParallelPostgresLive compares the production
// guarded four-reader path with the serial SQL on one unchanged database. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN and the explicit disposable opt-in to run.
func TestInvestigateCodeTopicGuardedParallelPostgresLive(t *testing.T) {
	adminDSN := os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN")
	ctx, database := postgresproof.OpenDisposableDatabase(t, adminDSN,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"), 2*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	_, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
VALUES
 ('repo-a', 'src/alpha.go', 'beta gamma', 'a', 12, 'go', now()),
 ('repo-a', 'src/content.go', 'alpha beta', 'b', 4, 'go', now()),
 ('repo-a', 'src/alpha-both.go', 'alpha delta', 'c', 2, 'go', now()),
 ('repo-a', 'src/null.txt', 'alpha', 'd', 1, NULL, now()),
 ('repo-b', 'src/beta.go', 'gamma', 'e', 5, 'go', now()),
 ('repo-hidden', 'src/alpha.go', 'beta gamma', 'f', 1, 'go', now());
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
 start_line, end_line, language, source_cache, indexed_at)
VALUES
 ('entity-a', 'repo-a', 'src/alpha.go', 'Function', 'alphaHandler', 2, 5, 'go', 'beta gamma', now()),
 ('entity-b', 'repo-a', 'src/content.go', 'Function', 'helper', 1, 3, 'go', 'alpha beta', now()),
 ('entity-null', 'repo-a', 'src/null.txt', 'Function', 'alphaNull', 1, 1, NULL, 'beta', now()),
 ('entity-other', 'repo-b', 'src/beta.go', 'Function', 'betaHandler', 1, 2, 'go', 'gamma', now()),
 ('entity-hidden', 'repo-hidden', 'src/alpha.go', 'Function', 'alphaHidden', 1, 1, 'go', 'beta', now())`)
	if err != nil {
		t.Fatalf("seed code topic fixture: %v", err)
	}

	// The helper intentionally returns only *sql.DB. Derive the runtime endpoint
	// from its actual database name, retaining the caller's host and credentials.
	var databaseName string
	if err := database.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatalf("read disposable database name: %v", err)
	}
	endpoint, err := url.Parse(adminDSN)
	if err != nil || (endpoint.Scheme != "postgres" && endpoint.Scheme != "postgresql") {
		t.Fatal("administrative endpoint must be a PostgreSQL URL")
	}
	endpoint.Path = "/" + databaseName
	endpoint.RawPath = ""
	query := endpoint.Query()
	query.Del("dbname")
	endpoint.RawQuery = query.Encode()
	targetDSN := endpoint.String()
	cfg, err := runtimepostgres.LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN", "ESHU_POSTGRES_READ_DSN":
			return targetDSN
		case "ESHU_POSTGRES_MAX_OPEN_CONNS":
			return "8"
		case "ESHU_POSTGRES_READ_MAX_OPEN_CONNS":
			return "4"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("load guarded reader config: %v", err)
	}
	observer := new(codeTopicGuardedReadObserver)
	access, err := runtimepostgres.Open(ctx, cfg, observer)
	if err != nil {
		t.Fatalf("open guarded reader: %v", err)
	}
	t.Cleanup(func() {
		if err := access.Close(); err != nil {
			t.Errorf("close guarded reader: %v", err)
		}
	})
	var guardedDatabaseName string
	if err := access.Writer().QueryRowContext(ctx, "SELECT current_database()").Scan(&guardedDatabaseName); err != nil || guardedDatabaseName != databaseName {
		t.Fatalf("guarded runtime selected disposable database: matched=%t err=%v", guardedDatabaseName == databaseName, err)
	}
	readStore := access.Reader()
	parallelStore, ok := readStore.(db.ReadSnapshotSetBeginner)
	if !ok || parallelStore.MaxReadConnections() < 4 {
		t.Fatalf("guarded reader snapshot capacity: implemented=%t", ok)
	}
	guarded := NewContentReaderWithReadStore(readStore)
	serial := NewContentReader(database)
	terms := []string{
		"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta",
		"iota", "kappa", "lambda", "mu", "nu", "xi", "omicron", "pi",
	}

	for _, tc := range []struct {
		name      string
		repoID    string
		grants    []string
		lang      string
		wantRows  int
		wantNulls bool
	}{
		{name: "unscoped", wantRows: 11, wantNulls: true},
		{name: "grant_and_language", grants: []string{"repo-a"}, lang: "go", wantRows: 5},
		{name: "null_language", grants: []string{"repo-a"}, wantRows: 7, wantNulls: true},
		{name: "specific_repository", repoID: "repo-b", wantRows: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := CodeTopicInvestigationRequest{
				Terms: terms, RepoID: tc.repoID,
				AllowedRepositoryIDs: tc.grants, Language: tc.lang, Limit: 100,
			}
			before := observer.borrows.Load()
			checkedCtx, err := access.ContextWithCheckpoint(ctx)
			if err != nil {
				t.Fatalf("writer checkpoint: %v", err)
			}
			want, err := serial.InvestigateCodeTopic(ctx, req)
			if err != nil {
				t.Fatalf("serial full result: %v", err)
			}
			got, err := guarded.InvestigateCodeTopic(checkedCtx, req)
			if err != nil {
				t.Fatalf("guarded full result: %v", err)
			}
			if observer.borrows.Load()-before < 4 {
				t.Fatal("guarded reader did not reserve four snapshot connections")
			}
			if len(got) != tc.wantRows || !reflect.DeepEqual(got, want) {
				t.Fatalf("complete ordered rows: guarded=%#v serial=%#v", got, want)
			}
			var files, entities, nullLanguages int
			for _, row := range got {
				if row.PoolTruncated || (len(tc.grants) > 0 && row.RepoID != "repo-a") ||
					(tc.repoID != "" && row.RepoID != tc.repoID) {
					t.Fatalf("unexpected grant or pool status: %#v", row)
				}
				switch row.SourceKind {
				case "file":
					files++
				case "entity":
					entities++
				default:
					t.Fatalf("unexpected source kind: %#v", row)
				}
				if row.Language == "" {
					nullLanguages++
				}
			}
			if files == 0 || entities == 0 || (nullLanguages == 2) != tc.wantNulls {
				t.Fatalf("fixture coverage: files=%d entities=%d null_languages=%d", files, entities, nullLanguages)
			}
			var paged []CodeTopicEvidenceRow
			for offset := 0; offset < len(want)+2; offset += 2 {
				req.Limit, req.Offset = 2, offset
				serialPage, serialErr := serial.InvestigateCodeTopic(ctx, req)
				guardedPage, guardedErr := guarded.InvestigateCodeTopic(checkedCtx, req)
				if serialErr != nil || guardedErr != nil || !reflect.DeepEqual(guardedPage, serialPage) {
					t.Fatalf("page offset %d: guarded=%#v err=%v serial=%#v err=%v",
						offset, guardedPage, guardedErr, serialPage, serialErr)
				}
				paged = append(paged, guardedPage...)
			}
			if !reflect.DeepEqual(paged, want) {
				t.Fatalf("assembled pages=%#v, full result=%#v", paged, want)
			}
		})
	}
}
