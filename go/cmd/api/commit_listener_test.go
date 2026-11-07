// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func testCommitLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestNormalizeCommitAddr pins the listener gate: unset stays dark, garbage
// fails fast, and a bare port binds as given. The host is never rewritten:
// the satellite plane is a separate pod, so loopback-forcing would break
// the only legitimate caller.
func TestNormalizeCommitAddr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
		fail bool
	}{
		{name: "unset stays dark", raw: "", want: ""},
		{name: "blank stays dark", raw: "  ", want: ""},
		{name: "bare port binds as given", raw: ":8089", want: ":8089"},
		{name: "loopback preserved", raw: "127.0.0.1:8089", want: "127.0.0.1:8089"},
		{name: "all-interfaces preserved", raw: "0.0.0.0:8089", want: "0.0.0.0:8089"},
		{name: "garbage fails", raw: "nope", fail: true},
		{name: "hostname refused", raw: "localhost:8089", fail: true},
		{name: "missing port fails", raw: "host:", fail: true},
		{name: "bad port fails", raw: ":99999", fail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeCommitAddr(tc.raw)
			if tc.fail {
				if err == nil {
					t.Fatalf("normalizeCommitAddr(%q) error = nil, want failure", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeCommitAddr(%q) error = %v, want nil", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeCommitAddr(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func commitListenerTestEnv() map[string]string {
	return map[string]string{
		"ESHU_COMMIT_LISTEN":          ":8089",
		"ESHU_COMMIT_MANIFEST_PATH":   "testdata/commit_listener_manifest.json",
		"ESHU_COMMIT_GRANTS_PATH":     "testdata/commit_listener_grants.json",
		"ESHU_COMMIT_CLAIM_PLANE_URL": "http://eshu-workflow-claim-plane:8080",
		"ESHU_COMMIT_SOURCE_SYSTEM":   "test-system",
		"ESHU_COMMIT_SCOPE_KIND":      "probe-scope",
		"ESHU_COMMIT_INSTANCE_ID":     "probe-instance-1",
	}
}

func commitListenerGetenv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func commitListenerErr(env map[string]string) error {
	_, err := loadCommitConfig(commitListenerGetenv(env))
	return err
}

// TestLoadCommitConfigDark proves the listener stays dark when the gate env
// is unset: no files are read and no error is raised.
func TestLoadCommitConfigDark(t *testing.T) {
	t.Parallel()

	cfg, err := loadCommitConfig(commitListenerGetenv(map[string]string{}))
	if err != nil {
		t.Fatalf("loadCommitConfig() error = %v, want nil", err)
	}
	if cfg != nil {
		t.Fatalf("loadCommitConfig() = %+v, want nil (dark)", cfg)
	}
}

// TestLoadCommitConfigRefusesWithoutManifest proves fail-closed startup:
// an enabled listener with no manifest path never builds.
func TestLoadCommitConfigRefusesWithoutManifest(t *testing.T) {
	t.Parallel()

	env := commitListenerTestEnv()
	delete(env, "ESHU_COMMIT_MANIFEST_PATH")
	err := commitListenerErr(env)
	if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_MANIFEST_PATH") {
		t.Fatalf("loadCommitConfig() error = %v, want the manifest variable named", err)
	}
}

// TestLoadCommitConfigRefusesWithoutPlaneURL proves the handler never
// builds without the plane it must fence against.
func TestLoadCommitConfigRefusesWithoutPlaneURL(t *testing.T) {
	t.Parallel()

	env := commitListenerTestEnv()
	delete(env, "ESHU_COMMIT_CLAIM_PLANE_URL")
	err := commitListenerErr(env)
	if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_CLAIM_PLANE_URL") {
		t.Fatalf("loadCommitConfig() error = %v, want the plane URL variable named", err)
	}
}

// TestLoadCommitConfigRefusesGarbagePlaneURL proves a malformed plane URL
// is half-configuration: startup refuses it with the variable named,
// before any socket opens, instead of serving commits that all fail later.
func TestLoadCommitConfigRefusesGarbagePlaneURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{":::not-a-url", "notaurl", "http://", "ftp://host/x"} {
		env := commitListenerTestEnv()
		env["ESHU_COMMIT_CLAIM_PLANE_URL"] = raw
		err := commitListenerErr(env)
		if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_CLAIM_PLANE_URL") {
			t.Errorf("loadCommitConfig() with plane URL %q error = %v, want the plane URL variable named", raw, err)
		}
	}
}

// TestLoadCommitConfigManifestErrorNamesVariable proves manifest read and
// parse failures meet the same bar as the grants path: the error names
// the variable and the path.
func TestLoadCommitConfigManifestErrorNamesVariable(t *testing.T) {
	t.Parallel()

	env := commitListenerTestEnv()
	env["ESHU_COMMIT_MANIFEST_PATH"] = "testdata/does-not-exist.json"
	err := commitListenerErr(env)
	if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_MANIFEST_PATH") ||
		!strings.Contains(err.Error(), "testdata/does-not-exist.json") {
		t.Errorf("loadCommitConfig() error = %v, want the manifest variable and path named", err)
	}

	corrupt := filepath.Join(t.TempDir(), "corrupt.json")
	if werr := os.WriteFile(corrupt, []byte("{not json"), 0o600); werr != nil {
		t.Fatalf("write corrupt manifest: %v", werr)
	}
	env["ESHU_COMMIT_MANIFEST_PATH"] = corrupt
	err = commitListenerErr(env)
	if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_MANIFEST_PATH") {
		t.Errorf("loadCommitConfig() error = %v, want the manifest variable named", err)
	}
}

// TestLoadCommitConfigRefusesWithoutIdentity proves the producer binding
// is complete or the listener never builds.
func TestLoadCommitConfigRefusesWithoutIdentity(t *testing.T) {
	t.Parallel()

	env := commitListenerTestEnv()
	delete(env, "ESHU_COMMIT_INSTANCE_ID")
	err := commitListenerErr(env)
	if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_INSTANCE_ID") {
		t.Fatalf("loadCommitConfig() error = %v, want the instance variable named", err)
	}
}

// TestAwaitCommitShutdownCancelsOnUnexpectedServeError replays the #10338
// P1 interleaving: the commit watcher closes commitDone only on ctx.Done
// (as in main.go), the main bind fails, and nothing else cancels. The
// fixed code cancels unconditionally; the old diagnostics-gated cancel
// hung here forever, so the timeout guard IS the regression assertion.
func TestAwaitCommitShutdownCancelsOnUnexpectedServeError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	commitDone := make(chan struct{})
	go func() {
		defer close(commitDone)
		<-ctx.Done()
	}()
	done := make(chan bool, 1)
	go func() {
		done <- awaitCommitShutdown(ctx, cancel, errors.New("listen tcp :8080: bind: address already in use"), commitDone)
	}()
	select {
	case failed := <-done:
		if !failed {
			t.Fatal("awaitCommitShutdown() = false, want true for an unexpected serve error")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("awaitCommitShutdown() hung: context was not cancelled on unexpected serve error")
	}
}

// TestAwaitCommitShutdownCleanOnServerClosed proves the graceful path is
// untouched: a signal cancel closes commitDone through the watcher and
// the join reports non-fatal.
func TestAwaitCommitShutdownCleanOnServerClosed(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	commitDone := make(chan struct{})
	go func() {
		defer close(commitDone)
		<-ctx.Done()
	}()
	cancel()
	done := make(chan bool, 1)
	go func() {
		done <- awaitCommitShutdown(ctx, cancel, http.ErrServerClosed, commitDone)
	}()
	select {
	case failed := <-done:
		if failed {
			t.Fatal("awaitCommitShutdown() = true, want false for http.ErrServerClosed")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("awaitCommitShutdown() hung on the clean shutdown path")
	}
}

// TestCommitServeFailed pins the fail-closed serve contract: only the
// clean shutdown return keeps the process alive.
func TestCommitServeFailed(t *testing.T) {
	t.Parallel()

	if commitServeFailed(nil) {
		t.Fatal("commitServeFailed(nil) = true, want false (nothing failed)")
	}
	if commitServeFailed(http.ErrServerClosed) {
		t.Fatal("commitServeFailed(ErrServerClosed) = true, want false (clean shutdown)")
	}
	if !commitServeFailed(errors.New("listen tcp :8089: bind: address already in use")) {
		t.Fatal("commitServeFailed(unexpected) = false, want true (fail the process)")
	}
}

// TestLoadCommitConfigRefusesWithoutSourceSystem proves the producer
// binding is complete or the listener never builds.
func TestLoadCommitConfigRefusesWithoutSourceSystem(t *testing.T) {
	t.Parallel()

	env := commitListenerTestEnv()
	delete(env, "ESHU_COMMIT_SOURCE_SYSTEM")
	err := commitListenerErr(env)
	if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_SOURCE_SYSTEM") {
		t.Fatalf("loadCommitConfig() error = %v, want the source-system variable named", err)
	}
}

// TestLoadCommitConfigRefusesWithoutScopeKind proves the scope binding is
// complete or the listener never builds.
func TestLoadCommitConfigRefusesWithoutScopeKind(t *testing.T) {
	t.Parallel()

	env := commitListenerTestEnv()
	delete(env, "ESHU_COMMIT_SCOPE_KIND")
	err := commitListenerErr(env)
	if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_SCOPE_KIND") {
		t.Fatalf("loadCommitConfig() error = %v, want the scope-kind variable named", err)
	}
}

// TestLoadCommitConfigRefusesMalformedGrants proves a set-but-broken
// grants file refuses with the variable named, not a bare JSON error.
func TestLoadCommitConfigRefusesMalformedGrants(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/grants.json"
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
	env := commitListenerTestEnv()
	env["ESHU_COMMIT_GRANTS_PATH"] = path
	err := commitListenerErr(env)
	if err == nil || !strings.Contains(err.Error(), "ESHU_COMMIT_GRANTS_PATH") {
		t.Fatalf("loadCommitConfig() error = %v, want the grants variable named", err)
	}
}

// TestLoadCommitConfigLoadsManifestAndGrants proves the file inputs land
// in the config the handler is built from.
func TestLoadCommitConfigLoadsManifestAndGrants(t *testing.T) {
	t.Parallel()

	cfg, err := loadCommitConfig(commitListenerGetenv(commitListenerTestEnv()))
	if err != nil {
		t.Fatalf("loadCommitConfig() error = %v, want nil", err)
	}
	if cfg == nil {
		t.Fatal("loadCommitConfig() = nil, want config")
	}
	if cfg.manifest.Metadata.ID != "dev.eshu.test.commitprobe" {
		t.Fatalf("manifest id = %q, want the testdata producer", cfg.manifest.Metadata.ID)
	}
	if len(cfg.grants) != 1 || cfg.grants[0].ProducerID != "dev.eshu.test.commitprobe" {
		t.Fatalf("grants = %+v, want the one testdata grant", cfg.grants)
	}
	if cfg.sourceSystem != "test-system" || string(cfg.scopeKind) != "probe-scope" || cfg.instanceID != "probe-instance-1" {
		t.Fatalf("binding = %+v, want the env identity", cfg)
	}
}

// TestBuildCommitMuxServesOnlyProtocolRoute proves the mount serves the
// protocol route through the real handler and nothing else: garbage on the
// route reaches admission (400, not 404) while other paths 404. The lazily
// opened database is never touched: admission refuses before any probe or
// commit, so no live Postgres is needed.
func TestBuildCommitMuxServesOnlyProtocolRoute(t *testing.T) {
	t.Parallel()

	cfg, err := loadCommitConfig(commitListenerGetenv(commitListenerTestEnv()))
	if err != nil {
		t.Fatalf("loadCommitConfig() error = %v, want nil", err)
	}
	database, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	defer func() { _ = database.Close() }()
	plane := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer plane.Close()

	mux, err := buildCommitMux(cfg, db.ExecQueryer(postgres.SQLDB{DB: database}), plane.URL, testCommitLogger())
	if err != nil {
		t.Fatalf("buildCommitMux() error = %v, want nil", err)
	}
	route := httptest.NewRequest(http.MethodPost, "/workflow.v1/CommitClaimFacts", strings.NewReader("garbage"))
	routeRecorder := httptest.NewRecorder()
	mux.ServeHTTP(routeRecorder, route)
	if routeRecorder.Code != http.StatusBadRequest {
		t.Fatalf("protocol route status = %d, want 400 (handler reached, not 404)", routeRecorder.Code)
	}
	for _, probe := range []struct {
		method string
		target string
	}{
		{http.MethodGet, "/workflow.v1/CommitClaimFacts"},
		{http.MethodPost, "/workflow.v1/Other"},
		{http.MethodGet, "/"},
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(probe.method, probe.target, nil))
		if recorder.Code != http.StatusNotFound && recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status = %d, want 404/405 (mux serves only the route)", probe.method, probe.target, recorder.Code)
		}
	}
}
