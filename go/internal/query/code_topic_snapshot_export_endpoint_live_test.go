// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7957 disposable PostgreSQL 18 primary and physical-standby proof that the real code-topic HTTP endpoint refuses shared-snapshot export on an undeclared reader build (permanent 503, no reader borrow) and serves the parallel shared-snapshot path once the exact build is declared; the live-postgres-readiness runner requires both administrative DSNs and explicit disposable opt-in, and verifies every test PASS event
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE ESHU_SCOPED_SEARCH_PROOF_DSN ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN ESHU_TEST_CONTENT_INDEX_POSTGRES_READ_DSN

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	runtimepostgres "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestCodeTopicSnapshotExportQualificationEndpointPostgresLive is the #7957
// capability-to-handler proof on a disposable primary and one direct physical
// standby: the real CodeHandler over a ContentReader on a real
// runtimepostgres.Access. Undeclared, a 16-term topic on a standby below
// PostgreSQL 18.7 answers the permanent 503 with no reader borrow and no
// business query; declaring the standby's exact server_version answers 200 on
// the parallel shared-snapshot path. A standby that already qualifies on the
// version floor skips the undeclared half. Both administrative DSNs must name
// postgres and the disposable opt-in must be set; never run it against a
// production writer.
func TestCodeTopicSnapshotExportQualificationEndpointPostgresLive(t *testing.T) {
	writerAdmin := os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN")
	readerAdmin := os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_READ_DSN")
	if readerAdmin == "" {
		t.Skip("set ESHU_TEST_CONTENT_INDEX_POSTGRES_READ_DSN to a direct physical standby")
	}
	readerURL, err := url.Parse(readerAdmin)
	if err != nil || (readerURL.Scheme != "postgres" && readerURL.Scheme != "postgresql") || readerURL.Path != "/postgres" {
		t.Fatal("reader administrative DSN must be a PostgreSQL URL for the postgres database")
	}
	readerPort, err := strconv.ParseUint(readerURL.Port(), 10, 16)
	if err != nil || readerPort == 0 || readerURL.Hostname() == "" {
		t.Fatal("reader administrative DSN must have a direct host and port")
	}
	ctx, database := postgresproof.OpenDisposableDatabase(t, writerAdmin,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"), 2*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
VALUES ('repo://tenant-a/granted', 'src/alpha.go', 'alpha beta gamma', 'a', 3, 'go', now())`); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	var databaseName string
	if err := database.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatalf("read disposable database name: %v", err)
	}
	writerDSN := codeTopicFleetDatabaseDSN(t, writerAdmin, databaseName)
	readerDSN := codeTopicFleetDatabaseDSN(t, readerAdmin, databaseName)
	standby, err := pgx.Connect(ctx, readerDSN)
	if err != nil {
		t.Fatalf("connect standby: %v", err)
	}
	var serverVersionNum int
	err = standby.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&serverVersionNum)
	_ = standby.Close(ctx)
	if err != nil {
		t.Fatalf("read standby server_version_num: %v", err)
	}
	terms := []string{
		"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "theta", "iota",
		"kappa", "lambda", "omicron", "sigma", "tau", "upsilon", "omega", "phi",
	}
	body, err := json.Marshal(map[string]any{"topic": "alpha", "terms": terms, "language": "go", "limit": 10})
	if err != nil {
		t.Fatal(err)
	}

	serve := func(t *testing.T, declare bool) (*httptest.ResponseRecorder, *codeTopicFleetObserver, map[string]string) {
		t.Helper()
		builds := ""
		if declare {
			builds = codeTopicFixtureSnapshotExportEnv(t, readerDSN)
		}
		cfg, err := runtimepostgres.LoadConfig(func(key string) string {
			switch key {
			case "ESHU_POSTGRES_DSN":
				return writerDSN
			case "ESHU_POSTGRES_READ_DSN":
				return readerDSN
			case "ESHU_POSTGRES_MAX_OPEN_CONNS", "ESHU_POSTGRES_MAX_IDLE_CONNS":
				return "8"
			case "ESHU_POSTGRES_READ_MAX_OPEN_CONNS", "ESHU_POSTGRES_READ_MAX_IDLE_CONNS":
				return "4"
			case "ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS":
				return builds
			default:
				return ""
			}
		})
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		observer := new(codeTopicFleetObserver)
		access, err := runtimepostgres.Open(ctx, cfg, observer)
		if err != nil {
			t.Fatalf("open standby access: %v", err)
		}
		t.Cleanup(func() { _ = access.Close() })
		spans := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
		content := NewContentReaderWithReadStore(access.Reader())
		content.tracer = provider.Tracer("snapshot-export-endpoint-live")
		handler := &CodeHandler{Content: content, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		checked, err := access.ContextWithCheckpoint(ctx)
		if err != nil {
			t.Fatalf("writer checkpoint: %v", err)
		}
		checked = ContextWithAuthContext(checked, testutil.CodeGrantScopedAuthContext([]string{"repo://tenant-a/granted"}))
		beforeBorrows, beforeQueries := observer.borrows.Load(), observer.businessQueries.Load()
		req := httptest.NewRequest(http.MethodPost, "/api/v0/code/topics/investigate", bytes.NewReader(body)).WithContext(checked)
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		delta := new(codeTopicFleetObserver)
		delta.borrows.Store(observer.borrows.Load() - beforeBorrows)
		delta.businessQueries.Store(observer.businessQueries.Load() - beforeQueries)
		attrs := map[string]string{}
		for _, span := range spans.Ended() {
			for _, kv := range span.Attributes() {
				if string(kv.Key) == "code_topic.execution_mode" || string(kv.Key) == "code_topic.denial_reason" {
					attrs[string(kv.Key)] = kv.Value.AsString()
				}
			}
		}
		return rec, delta, attrs
	}

	t.Run("undeclared", func(t *testing.T) {
		if serverVersionNum/10000 == 18 && serverVersionNum >= 180007 {
			t.Skipf("standby build %d qualifies on the version floor; the refusal is not reachable", serverVersionNum)
		}
		rec, delta, attrs := serve(t, false)
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "" {
			t.Fatalf("undeclared status=%d Retry-After=%q, want permanent 503; body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
		}
		var envelope ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable ||
			envelope.Error.Details["reason"] != querycontract.SnapshotExportUnqualifiedReason {
			t.Fatalf("undeclared envelope = %#v", envelope.Error)
		}
		if delta.borrows.Load() != 0 || delta.businessQueries.Load() != 0 {
			t.Fatalf("refused request borrowed %d readers and ran %d business queries", delta.borrows.Load(), delta.businessQueries.Load())
		}
		if attrs["code_topic.execution_mode"] != "denied" || attrs["code_topic.denial_reason"] != "snapshot_export_unqualified" {
			t.Fatalf("undeclared span attributes = %v", attrs)
		}
	})
	t.Run("declared", func(t *testing.T) {
		rec, delta, attrs := serve(t, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("declared status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if attrs["code_topic.execution_mode"] != "parallel_shared_snapshot" {
			t.Fatalf("declared span attributes = %v, want parallel_shared_snapshot", attrs)
		}
		if delta.borrows.Load() < 4 || delta.businessQueries.Load() < 5 {
			t.Fatalf("declared request borrowed %d readers and ran %d business queries, want four snapshot readers", delta.borrows.Load(), delta.businessQueries.Load())
		}
	})
}
