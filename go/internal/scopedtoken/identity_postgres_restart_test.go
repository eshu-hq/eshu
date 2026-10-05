// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestIdentityTokenRecoversAfterPostgresRestart is the #7586 regression with
// the API stack held constant: one pgaccess.Access, one identity resolver, one
// auth middleware, and one handler that takes the writer checkpoint a fenced
// read route takes. PostgreSQL is stopped and started underneath them. The same
// credential must answer a retryable 503 while the database is down and 200
// again once it is back, with no process restart; an invalid credential must
// still 401 against the recovered store.
//
// It needs an owned disposable PostgreSQL container it may stop, so it skips
// unless the fixture is named, like the restart tests in
// go/internal/runtime/postgres. Set ESHU_IDENTITY_RESTART_TEST=1,
// ESHU_IDENTITY_RESTART_TEST_DSN to the container's DSN (a role that can create
// databases), and ESHU_IDENTITY_RESTART_TEST_CONTAINER to its name. Each run
// creates and drops its own database, because a local identity bootstrap can
// complete only once per database.
func TestIdentityTokenRecoversAfterPostgresRestart(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_IDENTITY_RESTART_TEST_DSN"))
	container := strings.TrimSpace(os.Getenv("ESHU_IDENTITY_RESTART_TEST_CONTAINER"))
	if os.Getenv("ESHU_IDENTITY_RESTART_TEST") != "1" || dsn == "" || container == "" {
		t.Skip("owned PostgreSQL restart needs ESHU_IDENTITY_RESTART_TEST=1, _DSN, and _CONTAINER")
	}

	runDSN := createRunDatabase(t, dsn)
	cfg, err := pgaccess.LoadConfig(func(key string) string {
		if key == "ESHU_POSTGRES_DSN" || key == "ESHU_POSTGRES_READ_DSN" {
			return runDSN
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	access, err := pgaccess.Open(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("open postgres access: %v", err)
	}
	defer func() { _ = access.Close() }()

	writer := pgstatus.SQLDB{DB: access.Writer()}
	if err := pgstatus.ApplyBootstrap(ctx, writer); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	credential := seedIdentityToken(ctx, t, writer)

	resolver := NewPostgresIdentityResolver(pgstatus.NewScopedAPITokenStore(writer))
	// The handler takes the same writer checkpoint a fenced read route takes, so
	// a wedged checkpoint cannot hide behind a recovered identity read.
	fenced := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := access.ContextWithCheckpoint(r.Context()); err != nil {
			querycontract.WriteError(w, http.StatusServiceUnavailable, "writer checkpoint unavailable")
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	api := query.AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement("", resolver, fenced, nil, true)
	serve := func(bearer string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec.Code, rec.Header().Get("Retry-After")
	}

	if code, _ := serve(credential); code != http.StatusOK {
		t.Fatalf("before the outage: status = %d, want 200", code)
	}

	docker(t, "stop", container)
	code, retryAfter := serve(credential)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("during the outage: status = %d, want 503 (a 401 reads as a credential problem)", code)
	}
	if want := strconv.Itoa(querycontract.BackendUnavailableRetryAfterSeconds); retryAfter != want {
		t.Fatalf("during the outage: Retry-After = %q, want %q", retryAfter, want)
	}
	readyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	if err := access.Ping(readyCtx); err == nil {
		cancel()
		t.Fatal("during the outage: readiness Ping succeeded, want not ready")
	}
	cancel()

	docker(t, "start", container)
	deadline := time.Now().Add(60 * time.Second)
	var last int
	for time.Now().Before(deadline) {
		if last, _ = serve(credential); last == http.StatusOK {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if last != http.StatusOK {
		t.Fatalf("after PostgreSQL restarted on the same process: status = %d, want 200 (the process must recover without a restart)", last)
	}
	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPing()
	if err := access.Ping(pingCtx); err != nil {
		t.Fatalf("after recovery: readiness Ping = %v, want ready", err)
	}
	if code, _ := serve("not-a-real-credential"); code != http.StatusUnauthorized {
		t.Fatalf("invalid credential after recovery: status = %d, want 401", code)
	}
}

// createRunDatabase creates a uniquely named database on the fixture, drops it
// when the test ends, and returns the DSN that targets it.
func createRunDatabase(t *testing.T, dsn string) string {
	t.Helper()
	name := "restart_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open fixture admin connection: %v", err)
	}
	defer func() { _ = admin.Close() }()
	if _, err := admin.ExecContext(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create run database: %v", err)
	}
	t.Cleanup(func() {
		cleaner, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Errorf("open fixture cleanup connection: %v", err)
			return
		}
		defer func() { _ = cleaner.Close() }()
		if _, err := cleaner.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop run database %s: %v", name, err)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse fixture DSN: %v", err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

// seedIdentityToken bootstraps a local owner identity and returns a personal API
// token credential the identity resolver accepts.
func seedIdentityToken(ctx context.Context, t *testing.T, writer pgstatus.SQLDB) string {
	t.Helper()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	now := time.Now().UTC()
	store := pgstatus.NewIdentitySubjectStore(writer)
	tenant, workspace, user := "tenant_"+suffix, "workspace_"+suffix, "user_"+suffix
	if err := store.BootstrapLocalIdentity(ctx, pgstatus.LocalIdentityBootstrapRecord{
		TenantID:               tenant,
		WorkspaceID:            workspace,
		UserID:                 user,
		SubjectIDHash:          "sha256:subject-" + suffix,
		ProfileHandleHash:      "sha256:handle-" + suffix,
		PasswordHash:           "sha256:password-" + suffix,
		PasswordAlgorithm:      "argon2id",
		PasswordParametersHash: "sha256:params-" + suffix,
		MFAFactorID:            "mfa_" + suffix,
		MFAFactorKind:          "totp",
		MFACredentialHandle:    "handle_" + suffix,
		RecoveryCodeHashes:     []string{"sha256:recovery-" + suffix},
		PolicyRevisionHash:     "sha256:policy-" + suffix,
		CreatedAt:              now,
	}); err != nil {
		t.Fatalf("bootstrap local identity: %v", err)
	}
	credential := "restart-credential-" + suffix
	if err := store.CreateLocalIdentityAPIToken(ctx, pgstatus.LocalIdentityAPITokenCreate{
		TokenID:            "token_" + suffix,
		TokenHash:          pgstatus.ScopedAPITokenHash(credential),
		TokenClass:         "personal",
		TenantID:           tenant,
		WorkspaceID:        workspace,
		UserID:             user,
		DisplayHandleHash:  "sha256:display-" + suffix,
		DisplayLabel:       "restart regression",
		PolicyRevisionHash: "sha256:policy-" + suffix,
		IssuedAt:           now,
		ExpiresAt:          now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("create identity api token: %v", err)
	}
	return credential
}

func docker(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("docker %s: %v %s", strings.Join(args, " "), err, out)
	}
}
