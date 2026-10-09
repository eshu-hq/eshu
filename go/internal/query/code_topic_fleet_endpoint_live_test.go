// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	runtimepostgres "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

type codeTopicFleetObserver struct {
	businessQueries atomic.Int64
	borrows         atomic.Int64
}

// codeTopicFleetProbeGate holds each real standby probe until all four have
// entered QueryContext. A serial partition loop times out instead of passing
// on the same query and borrow totals as parallel execution.
type codeTopicFleetProbeGate struct {
	arrived atomic.Int32
	release chan struct{}
}

type codeTopicFleetProbeStore struct {
	db.ReadStore
	snapshots db.ReadSnapshotSetBeginner
	gate      atomic.Pointer[codeTopicFleetProbeGate]
}

func (s *codeTopicFleetProbeStore) MaxReadConnections() int {
	return s.snapshots.MaxReadConnections()
}

func (s *codeTopicFleetProbeStore) BeginReadOnlySnapshotSet(ctx context.Context, count int) (db.ReadSnapshotSet, error) {
	set, err := s.snapshots.BeginReadOnlySnapshotSet(ctx, count)
	if err != nil {
		return nil, err
	}
	return codeTopicFleetProbeSet{ReadSnapshotSet: set, gate: s.gate.Load()}, nil
}

type codeTopicFleetProbeSet struct {
	db.ReadSnapshotSet
	gate *codeTopicFleetProbeGate
}

func (s codeTopicFleetProbeSet) Reader(index int) (db.Queryer, error) {
	reader, err := s.ReadSnapshotSet.Reader(index)
	if err != nil {
		return nil, err
	}
	return codeTopicFleetProbeReader{Queryer: reader, gate: s.gate}, nil
}

type codeTopicFleetProbeReader struct {
	db.Queryer
	gate *codeTopicFleetProbeGate
}

func (r codeTopicFleetProbeReader) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if r.gate != nil && strings.Contains(query, "WITH terms(term) AS") && !strings.Contains(query, "jsonb_to_recordset") {
		if r.gate.arrived.Add(1) == 4 {
			close(r.gate.release)
		}
		select {
		case <-r.gate.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, fmt.Errorf("four code-topic probes did not overlap: arrived=%d", r.gate.arrived.Load())
		}
	}
	return r.Queryer.QueryContext(ctx, query, args...)
}

func (o *codeTopicFleetObserver) Observe(role string, stage runtimepostgres.Stage, outcome runtimepostgres.Outcome, _ time.Duration) {
	if role == "reader" && stage == runtimepostgres.StageReaderBorrow && outcome == runtimepostgres.OutcomeOK {
		o.borrows.Add(1)
	}
	if role == "reader" && stage == runtimepostgres.StageBusinessQuery && outcome == runtimepostgres.OutcomeOK {
		o.businessQueries.Add(1)
	}
}

// TestCodeTopicFleetCapacityEndpointPostgresLive proves the actual HTTP handler
// and runtime fleet reader on a disposable database replicated to one direct
// physical standby. Both administrative DSNs must name postgres; the explicit
// disposable opt-in protects against accidental database creation. Run only
// against an owned disposable primary and standby, never a production writer.
func TestCodeTopicFleetCapacityEndpointPostgresLive(t *testing.T) {
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
	_, err = database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
VALUES
 ('repo://tenant-a/granted', 'src/alpha.go', 'beta gamma', 'a', 5, 'go', now()),
 ('repo://tenant-a/granted', 'src/beta.go', 'alpha delta', 'b', 4, 'go', now()),
 ('repo://tenant-a/granted', 'src/alpha.py', 'alpha beta', 'c', 3, 'python', now()),
 ('repo://tenant-b/hidden', 'src/alpha.go', 'alpha beta', 'd', 2, 'go', now());
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
 start_line, end_line, language, source_cache, indexed_at)
VALUES
 ('entity-a', 'repo://tenant-a/granted', 'src/alpha.go', 'Function', 'alphaHandler', 1, 3, 'go', 'beta gamma', now()),
 ('entity-hidden', 'repo://tenant-b/hidden', 'src/alpha.go', 'Function', 'alphaHidden', 1, 2, 'go', 'beta', now());
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT 'repo://tenant-a/granted', 'dense/needle-' || lpad(i::text, 4, '0') || '.go',
       'other', 'dense', 1, 'go', now()
FROM generate_series(1, 251) AS i`)
	if err != nil {
		t.Fatalf("seed code-topic fleet fixture: %v", err)
	}
	var databaseName string
	if err := database.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatalf("read disposable database name: %v", err)
	}
	writerDSN := codeTopicFleetDatabaseDSN(t, writerAdmin, databaseName)
	readerDSN := codeTopicFleetDatabaseDSN(t, readerAdmin, databaseName)
	members, err := json.Marshal([]runtimepostgres.ReaderMember{{ID: "test-reader", Host: readerURL.Hostname(), Port: uint16(readerPort)}})
	if err != nil {
		t.Fatalf("encode reader member: %v", err)
	}
	cfg, err := runtimepostgres.LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writerDSN
		case "ESHU_POSTGRES_READ_DSN":
			return readerDSN
		case "ESHU_POSTGRES_READ_MEMBERS":
			return string(members)
		case "ESHU_POSTGRES_MAX_OPEN_CONNS":
			return "8"
		case "ESHU_POSTGRES_MAX_IDLE_CONNS":
			return "8"
		case "ESHU_POSTGRES_READ_MAX_OPEN_CONNS":
			return "4"
		case "ESHU_POSTGRES_READ_MAX_IDLE_CONNS":
			return "4"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("load fleet config: %v", err)
	}
	observer := new(codeTopicFleetObserver)
	access, err := runtimepostgres.Open(ctx, cfg, observer)
	if err != nil {
		t.Fatalf("open physical reader fleet: %v", err)
	}
	t.Cleanup(func() {
		if err := access.Close(); err != nil {
			t.Errorf("close fleet access: %v", err)
		}
	})
	reader := access.Reader()
	snapshotReader, ok := reader.(db.ReadSnapshotSetBeginner)
	if !ok {
		t.Fatal("fleet reader does not provide a snapshot set")
	}
	probeStore := &codeTopicFleetProbeStore{ReadStore: reader, snapshots: snapshotReader}
	handler := &CodeHandler{Content: NewContentReaderWithReadStore(probeStore), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	terms := []string{
		"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "theta", "iota",
		"kappa", "lambda", "omicron", "sigma", "tau", "upsilon", "omega", "phi",
	}

	for _, tc := range []struct {
		name          string
		terms         []string
		limit         int
		wantPoolCap   bool
		wantPageLimit bool
	}{
		{name: "granted_uncapped", terms: terms, limit: 10},
		{name: "granted_capped", terms: append([]string{"needle"}, terms[1:]...), limit: 10, wantPoolCap: true, wantPageLimit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestBody, err := json.Marshal(map[string]any{
				"topic": tc.terms[0], "terms": tc.terms, "language": "go", "limit": tc.limit,
			})
			if err != nil {
				t.Fatalf("encode request: %v", err)
			}
			post := func() map[string]any {
				t.Helper()
				checkedCtx, err := access.ContextWithCheckpoint(ctx)
				if err != nil {
					t.Fatalf("writer checkpoint: %v", err)
				}
				auth := testutil.CodeGrantScopedAuthContext([]string{"repo://tenant-a/granted"})
				checkedCtx = ContextWithAuthContext(checkedCtx, auth)
				req := httptest.NewRequest(http.MethodPost, "/api/v0/code/topics/investigate", bytes.NewReader(requestBody)).WithContext(checkedCtx)
				req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("POST /api/v0/code/topics/investigate status=%d, want 200: %s", rec.Code, rec.Body.String())
				}
				var response ResponseEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				data, ok := response.Data.(map[string]any)
				if !ok {
					t.Fatalf("response data type %T, want map", response.Data)
				}
				return data
			}
			beforeHealthyQueries := observer.businessQueries.Load()
			beforeHealthyBorrows := observer.borrows.Load()
			healthyGate := &codeTopicFleetProbeGate{release: make(chan struct{})}
			probeStore.gate.Store(healthyGate)
			healthy := post()
			if got := healthyGate.arrived.Load(); got != 4 {
				t.Fatalf("healthy concurrent probe arrivals=%d, want four", got)
			}
			probeStore.gate.Store(nil)
			if got := observer.businessQueries.Load() - beforeHealthyQueries; got < 5 {
				t.Fatalf("healthy reader query operations=%d, want at least five", got)
			}
			if got := observer.borrows.Load() - beforeHealthyBorrows; got < 4 {
				t.Fatalf("healthy reader borrows=%d, want four snapshot readers", got)
			}
			coverage, ok := healthy["coverage"].(map[string]any)
			if !ok {
				t.Fatalf("healthy coverage=%#v, want object", healthy["coverage"])
			}
			if got := coverage["searched_term_count"]; got != float64(16) {
				t.Fatalf("searched_term_count=%v, want 16 to exercise four-reader fleet", got)
			}
			if got := healthy["candidate_pool_truncated"]; got != tc.wantPoolCap {
				t.Fatalf("healthy candidate_pool_truncated=%v, want %t", got, tc.wantPoolCap)
			}
			if got := healthy["truncated"]; got != tc.wantPageLimit {
				t.Fatalf("healthy truncated=%v, want %t", got, tc.wantPageLimit)
			}
			groups, ok := healthy["evidence_groups"].([]any)
			if !ok || len(groups) == 0 {
				t.Fatalf("healthy evidence_groups=%#v, want nonempty", healthy["evidence_groups"])
			}
			for _, raw := range groups {
				row, ok := raw.(map[string]any)
				if !ok {
					t.Fatalf("healthy evidence row=%#v, want object", raw)
				}
				if row["repo_id"] != "repo://tenant-a/granted" || row["language"] != "go" {
					t.Fatalf("row escaped grant or language filter: %#v", row)
				}
			}
			var holders []interface{ Rollback() error }
			t.Cleanup(func() {
				for _, holder := range holders {
					if holder == nil {
						continue
					}
					if err := holder.Rollback(); err != nil {
						t.Errorf("cleanup held reader: %v", err)
					}
				}
			})
			for range 3 {
				checkedCtx, err := access.ContextWithCheckpoint(ctx)
				if err != nil {
					t.Fatalf("holder writer checkpoint: %v", err)
				}
				holder, err := reader.BeginReadOnlySnapshot(checkedCtx)
				if err != nil {
					t.Fatalf("hold reader slot: %v", err)
				}
				holders = append(holders, holder)
			}
			beforeQueries := observer.businessQueries.Load()
			pressured := post()
			if !reflect.DeepEqual(pressured, healthy) {
				t.Fatalf("pressured response differs from healthy: got=%#v want=%#v", pressured, healthy)
			}
			if got := observer.businessQueries.Load() - beforeQueries; got != 1 {
				t.Fatalf("pressure business queries=%d, want exactly one fenced fallback", got)
			}
			_, stats := access.Stats()
			if stats.InUse != 3 {
				t.Fatalf("after fallback reader InUse=%d, want three held slots", stats.InUse)
			}
			for index, holder := range holders {
				if err := holder.Rollback(); err != nil {
					t.Fatalf("release held reader: %v", err)
				}
				holders[index] = nil
			}
			_, stats = access.Stats()
			if stats.InUse != 0 {
				t.Fatalf("after release reader InUse=%d, want zero", stats.InUse)
			}
			beforeRecoveredQueries := observer.businessQueries.Load()
			beforeRecoveredBorrows := observer.borrows.Load()
			recoveredGate := &codeTopicFleetProbeGate{release: make(chan struct{})}
			probeStore.gate.Store(recoveredGate)
			if recovered := post(); !reflect.DeepEqual(recovered, healthy) {
				t.Fatalf("recovered response differs from healthy: got=%#v want=%#v", recovered, healthy)
			}
			if got := recoveredGate.arrived.Load(); got != 4 {
				t.Fatalf("recovered concurrent probe arrivals=%d, want four", got)
			}
			probeStore.gate.Store(nil)
			if got := observer.businessQueries.Load() - beforeRecoveredQueries; got < 5 {
				t.Fatalf("recovered reader query operations=%d, want at least five", got)
			}
			if got := observer.borrows.Load() - beforeRecoveredBorrows; got < 4 {
				t.Fatalf("recovered reader borrows=%d, want four snapshot readers", got)
			}
		})
	}
}

func codeTopicFleetDatabaseDSN(t *testing.T, adminDSN, databaseName string) string {
	t.Helper()
	endpoint, err := url.Parse(adminDSN)
	if err != nil || (endpoint.Scheme != "postgres" && endpoint.Scheme != "postgresql") || endpoint.Path != "/postgres" {
		t.Fatal("administrative DSN must be a PostgreSQL URL for the postgres database")
	}
	endpoint.Path = "/" + databaseName
	endpoint.RawPath = ""
	query := endpoint.Query()
	query.Del("dbname")
	endpoint.RawQuery = query.Encode()
	if endpoint.Hostname() == "" || endpoint.Port() == "" {
		t.Fatalf("administrative DSN must specify host and port for %s", databaseName)
	}
	return endpoint.String()
}
