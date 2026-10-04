// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	runtimepostgres "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type codeTopicTestMemberLost struct{}

func (codeTopicTestMemberLost) Error() string          { return "test reader member lost" }
func (codeTopicTestMemberLost) ReaderMemberLost() bool { return true }

type codeTopicRetryTestStore struct {
	codeTopicTestSnapshotStore
	attempts       int
	beginFailure   error
	failEveryBegin bool
}

func (store *codeTopicRetryTestStore) BeginReadOnlySnapshotSet(ctx context.Context, count int) (db.ReadSnapshotSet, error) {
	store.attempts++
	if (store.attempts == 1 || store.failEveryBegin) && store.beginFailure != nil {
		return nil, store.beginFailure
	}
	return store.codeTopicTestSnapshotStore.BeginReadOnlySnapshotSet(ctx, count)
}

func TestInvestigateCodeTopicMemberLossRetryIsBounded(t *testing.T) {
	recorder := &codeTopicParallelRecorder{}
	pool := openCodeTopicParallelDB(t, recorder)
	store := &codeTopicRetryTestStore{
		codeTopicTestSnapshotStore: codeTopicTestSnapshotStore{
			ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
		},
		beginFailure: codeTopicTestMemberLost{}, failEveryBegin: true,
	}
	reader := NewContentReaderWithReadStore(store)
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	_, err := reader.InvestigateCodeTopic(t.Context(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1})
	var lost codeTopicTestMemberLost
	if !errors.As(err, &lost) || store.attempts != 2 {
		t.Fatalf("bounded retry err=%v attempts=%d", err, store.attempts)
	}
}

func TestInvestigateCodeTopicRetriesWholeReadAfterMemberLoss(t *testing.T) {
	recorder := &codeTopicParallelRecorder{}
	pool := openCodeTopicParallelDB(t, recorder)
	store := &codeTopicRetryTestStore{
		codeTopicTestSnapshotStore: codeTopicTestSnapshotStore{
			ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
		},
		beginFailure: fmt.Errorf("wrapped member failure: %w", codeTopicTestMemberLost{}),
	}
	reader := NewContentReaderWithReadStore(store)
	terms := []string{"same", "other", "same", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	result, err := reader.InvestigateCodeTopic(t.Context(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1})
	if err != nil || len(result) != 1 {
		t.Fatalf("whole-read retry result=%#v err=%v", result, err)
	}
	if store.attempts != 2 {
		t.Fatalf("snapshot attempts=%d, want 2", store.attempts)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.probes != 4 || recorder.imports != 3 || recorder.active != 0 || recorder.assemblyJSON == "" {
		t.Fatalf("retry probes=%d imports=%d active=%d assembly=%q", recorder.probes, recorder.imports, recorder.active, recorder.assemblyJSON)
	}
}

func TestInvestigateCodeTopicDoesNotRetryMemberLossAfterCancellation(t *testing.T) {
	recorder := &codeTopicParallelRecorder{}
	pool := openCodeTopicParallelDB(t, recorder)
	store := &codeTopicRetryTestStore{
		codeTopicTestSnapshotStore: codeTopicTestSnapshotStore{
			ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
		},
		beginFailure: codeTopicTestMemberLost{},
	}
	reader := NewContentReaderWithReadStore(store)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	_, err := reader.InvestigateCodeTopic(ctx, codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1})
	var lost codeTopicTestMemberLost
	if !errors.As(err, &lost) || store.attempts != 1 {
		t.Fatalf("canceled read err=%v attempts=%d", err, store.attempts)
	}
}

func TestInvestigateCodeTopicRetriesAllProbesAfterMemberLoss(t *testing.T) {
	recorder := &codeTopicParallelRecorder{probeFailureOnce: codeTopicTestMemberLost{}}
	pool := openCodeTopicParallelDB(t, recorder)
	store := &codeTopicRetryTestStore{codeTopicTestSnapshotStore: codeTopicTestSnapshotStore{
		ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
	}}
	reader := NewContentReaderWithReadStore(store)
	terms := []string{"same", "other", "same", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	result, err := reader.InvestigateCodeTopic(t.Context(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1})
	if err != nil || len(result) != 1 {
		t.Fatalf("whole-read probe retry result=%#v err=%v", result, err)
	}
	if store.attempts != 2 {
		t.Fatalf("snapshot attempts=%d, want 2", store.attempts)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.probes < 5 || recorder.imports != 6 || recorder.active != 0 || recorder.assemblyJSON == "" {
		t.Fatalf("retry probes=%d imports=%d active=%d assembly=%q", recorder.probes, recorder.imports, recorder.active, recorder.assemblyJSON)
	}
}

func TestInvestigateCodeTopicParallelRollsBackAfterProbeError(t *testing.T) {
	recorder := &codeTopicParallelRecorder{probeFailure: errors.New("injected probe failure")}
	reader := newCodeTopicParallelTestReader(openCodeTopicParallelDB(t, recorder))
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	_, err := reader.InvestigateCodeTopic(context.Background(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 26})
	if err == nil || !strings.Contains(err.Error(), "injected probe failure") {
		t.Fatalf("error = %v, want injected failure", err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.active != 0 || recorder.assemblyJSON != "" {
		t.Fatalf("active=%d assembly=%q", recorder.active, recorder.assemblyJSON)
	}
}

var (
	_ error                      = codeTopicTestMemberLost{}
	_ db.ReadSnapshotSetBeginner = (*codeTopicRetryTestStore)(nil)
)

// memberLossLiveStore keeps the production Access and snapshot machinery in
// the test path. Only the first probe is replaced with a long-running read.
type memberLossLiveStore struct {
	db.ReadStore
	snapshots db.ReadSnapshotSetBeginner
	admin     map[string]*sql.DB
	database  string
	mu        sync.Mutex
	hosts     []string
	attempts  int
	failure   error
}

func (s *memberLossLiveStore) MaxReadConnections() int { return s.snapshots.MaxReadConnections() }

func (s *memberLossLiveStore) BeginReadOnlySnapshotSet(ctx context.Context, count int) (db.ReadSnapshotSet, error) {
	set, err := s.snapshots.BeginReadOnlySnapshotSet(ctx, count)
	if err != nil {
		return nil, err
	}
	reader, err := set.Reader(0)
	if err != nil {
		_ = set.Close()
		return nil, err
	}
	var host string
	if err := memberLossScanOne(ctx, reader, "SELECT host(inet_server_addr())", &host); err != nil {
		_ = set.Close()
		return nil, err
	}
	s.mu.Lock()
	s.attempts++
	s.hosts = append(s.hosts, host)
	interrupt := s.attempts == 1
	s.mu.Unlock()
	if interrupt {
		return &memberLossLiveSet{ReadSnapshotSet: set, store: s}, nil
	}
	return set, nil
}

type memberLossLiveSet struct {
	db.ReadSnapshotSet
	store *memberLossLiveStore
}

func (s *memberLossLiveSet) Reader(index int) (db.Queryer, error) {
	reader, err := s.ReadSnapshotSet.Reader(index)
	if err != nil || index != 0 {
		return reader, err
	}
	return memberLossLiveQueryer{Queryer: reader, store: s.store}, nil
}

type memberLossLiveQueryer struct {
	db.Queryer
	store *memberLossLiveStore
}

func memberLossScanOne(ctx context.Context, reader db.Queryer, statement string, dest ...any) error {
	rows, err := reader.QueryContext(ctx, statement)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	return rows.Err()
}

func (q memberLossLiveQueryer) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	var pid int
	var host string
	if err := memberLossScanOne(ctx, q.Queryer, "SELECT pg_backend_pid(), host(inet_server_addr())", &pid, &host); err != nil {
		return nil, err
	}
	admin := q.store.admin[host]
	if admin == nil {
		return nil, fmt.Errorf("no owned standby control for selected member")
	}
	controlCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	terminated := make(chan error, 1)
	go func() {
		for controlCtx.Err() == nil {
			var active bool
			err := admin.QueryRowContext(controlCtx,
				"SELECT state = 'active' AND query LIKE 'SELECT pg_sleep(30)%' AND datname = $2 FROM pg_stat_activity WHERE pid = $1",
				pid, q.store.database).Scan(&active)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				terminated <- err
				return
			}
			if active {
				var killed bool
				err = admin.QueryRowContext(controlCtx, "SELECT pg_terminate_backend($1)", pid).Scan(&killed)
				if err == nil && !killed {
					err = errors.New("owned standby backend was not terminated")
				}
				terminated <- err
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		terminated <- controlCtx.Err()
	}()
	rows, err := q.Queryer.QueryContext(controlCtx, "SELECT pg_sleep(30)")
	if err == nil {
		if rows.Next() {
			err = errors.New("interrupted read unexpectedly completed")
		} else {
			err = rows.Err()
		}
		_ = rows.Close()
	}
	if killErr := <-terminated; killErr != nil {
		return nil, killErr
	}
	q.store.mu.Lock()
	q.store.failure = err
	q.store.mu.Unlock()
	return nil, err
}

func TestInvestigateCodeTopicRetriesPhysicalMidReadMemberLoss(t *testing.T) {
	writerDSN := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	firstDSN := os.Getenv("ESHU_READER_TEST_READER_DSN")
	secondDSN := os.Getenv("ESHU_READER_TEST_SECOND_READER_DSN")
	if writerDSN == "" || firstDSN == "" || secondDSN == "" {
		t.Skip("owned primary and two physical standbys not configured")
	}
	if os.Getenv("ESHU_READER_TEST_TERMINATE_BACKEND") != "1" {
		t.Skip("explicit owned-standby backend-termination opt-in not set")
	}
	requireOwnedMemberLossFixture(t, []string{writerDSN, firstDSN, secondDSN})
	ctx, database := postgresproof.OpenDisposableDatabase(t, writerDSN,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"), 2*time.Minute)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
VALUES ('repo-a', 'src/alpha.go', 'alpha beta', 'a', 2, 'go', now());
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, indexed_at)
VALUES ('entity-a', 'repo-a', 'src/alpha.go', 'Function', 'alphaHandler', 1, 2, 'go', 'alpha beta', now())`); err != nil {
		t.Fatal(err)
	}
	var databaseName string
	if err := database.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "eshu_content_index_proof_") {
		t.Fatal("backend-termination target is not a disposable proof database")
	}
	withDatabase := func(raw string) string {
		endpoint, err := url.Parse(raw)
		if err != nil || (endpoint.Scheme != "postgres" && endpoint.Scheme != "postgresql") {
			t.Fatal("fixture DSN must be a PostgreSQL URL")
		}
		endpoint.Path, endpoint.RawPath = "/"+databaseName, ""
		params := endpoint.Query()
		params.Del("dbname")
		endpoint.RawQuery = params.Encode()
		return endpoint.String()
	}
	first := withDatabase(firstDSN)
	firstConfig, err := pgx.ParseConfig(firstDSN)
	if err != nil {
		t.Fatal(err)
	}
	secondConfig, err := pgx.ParseConfig(secondDSN)
	if err != nil {
		t.Fatal(err)
	}
	admin := make(map[string]*sql.DB, 2)
	for _, endpoint := range []struct{ dsn, host string }{{firstDSN, firstConfig.Host}, {secondDSN, secondConfig.Host}} {
		control, err := sql.Open("pgx", endpoint.dsn)
		if err != nil {
			t.Fatal(err)
		}
		admin[endpoint.host] = control
		t.Cleanup(func() { _ = control.Close() })
	}
	cfg, err := runtimepostgres.LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return withDatabase(writerDSN)
		case "ESHU_POSTGRES_READ_DSN":
			return first
		case "ESHU_POSTGRES_MAX_OPEN_CONNS":
			return "16"
		case "ESHU_POSTGRES_READ_MAX_OPEN_CONNS", "ESHU_POSTGRES_READ_MAX_IDLE_CONNS":
			return "8"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.ReadMembers = []runtimepostgres.ReaderMember{
		{ID: "first", Host: firstConfig.Host, Port: firstConfig.Port},
		{ID: "second", Host: secondConfig.Host, Port: secondConfig.Port},
	}
	access, err := runtimepostgres.Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = access.Close() })
	checkedCtx, err := access.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request := codequery.CodeTopicInvestigationRequest{Terms: []string{
		"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta",
		"iota", "kappa", "lambda", "mu", "nu", "xi", "omicron", "pi",
	}, Limit: 10}
	want, err := NewContentReaderWithReadStore(access.Reader()).InvestigateCodeTopic(checkedCtx, request)
	if err != nil {
		t.Fatal(err)
	}
	readStore := access.Reader()
	snapshots := readStore.(db.ReadSnapshotSetBeginner)
	interrupted := &memberLossLiveStore{ReadStore: readStore, snapshots: snapshots, admin: admin, database: databaseName}
	got, err := NewContentReaderWithReadStore(interrupted).InvestigateCodeTopic(checkedCtx, request)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("whole-workflow retry err=%v equal=%t", err, reflect.DeepEqual(got, want))
	}
	interrupted.mu.Lock()
	attempts := interrupted.attempts
	hosts := append([]string(nil), interrupted.hosts...)
	failure := interrupted.failure
	interrupted.mu.Unlock()
	var pgErr *pgconn.PgError
	var marker interface{ ReaderMemberLost() bool }
	if attempts != 2 || len(hosts) != 2 || hosts[0] == hosts[1] ||
		!errors.As(failure, &pgErr) || pgErr.Code != "57P01" ||
		!errors.As(failure, &marker) || !marker.ReaderMemberLost() {
		t.Fatalf("mid-read proof attempts=%d hosts=%v failure=%v", attempts, hosts, failure)
	}
	if _, stats := access.Stats(); stats.InUse != 0 {
		t.Fatalf("reader connections leaked: %+v", stats)
	}
	assertMemberLossReservationsZero(t, access)
	t.Logf("physical mid-read retry: code=%s attempts=%d distinct_members=true matching_response=true reader_in_use=0 reservations=0", pgErr.Code, attempts)
}
