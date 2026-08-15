// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestRunAttachedLocalMCPStdioUsesRecordedPostgresPort(t *testing.T) {
	layout := eshulocal.Layout{
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/tmp/owner.json",
	}
	record := eshulocal.OwnerRecord{
		PID:                42,
		WorkspaceID:        layout.WorkspaceID,
		PostgresPort:       15439,
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
	}

	originalReadOwnerRecord := ReadOwnerRecord
	originalProcessAlive := ProcessAlive
	originalSocketHealthy := SocketHealthy
	originalStartChild := StartChildProcess
	originalWaitChild := localHostWaitChildProcess
	t.Cleanup(func() {
		ReadOwnerRecord = originalReadOwnerRecord
		ProcessAlive = originalProcessAlive
		SocketHealthy = originalSocketHealthy
		StartChildProcess = originalStartChild
		localHostWaitChildProcess = originalWaitChild
	})

	ReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	ProcessAlive = func(pid int) bool {
		return pid == record.PID
	}
	SocketHealthy = func(path string) bool {
		return path == record.PostgresSocketPath
	}

	var gotEnv []string
	StartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		gotEnv = append([]string(nil), env...)
		return &exec.Cmd{}, nil
	}
	localHostWaitChildProcess = func(ctx context.Context, cmd *exec.Cmd) error {
		return nil
	}

	attached, err := RunAttachedMCPStdio(context.Background(), layout)
	if err != nil {
		t.Fatalf("RunAttachedMCPStdio() error = %v, want nil", err)
	}
	if !attached {
		t.Fatal("RunAttachedMCPStdio() attached = false, want true")
	}

	dsn := envValue(gotEnv, "ESHU_POSTGRES_DSN")
	if !strings.Contains(dsn, "host=127.0.0.1") || !strings.Contains(dsn, "port=15439") {
		t.Fatalf("ESHU_POSTGRES_DSN = %q, want loopback DSN with recorded port", dsn)
	}
	if got := envValue(gotEnv, "ESHU_MCP_TRANSPORT"); got != "stdio" {
		t.Fatalf("ESHU_MCP_TRANSPORT = %q, want stdio", got)
	}
	if got := envValue(gotEnv, "ESHU_LOCAL_LOG_MODE"); got != "terminal" {
		t.Fatalf("ESHU_LOCAL_LOG_MODE = %q, want terminal for MCP stdio", got)
	}
}

func TestLocalBootstrapDefinitionsCanDeferContentSearchIndexes(t *testing.T) {
	defs := localBootstrapDefinitions(func(key string) string {
		if key == deferContentSearchIndexesEnv {
			return "true"
		}
		return ""
	})

	var contentStoreSQL string
	for _, def := range defs {
		if def.Name == "content_store" {
			contentStoreSQL = def.SQL
			break
		}
	}
	if contentStoreSQL == "" {
		t.Fatal("content_store definition missing")
	}
	if !strings.Contains(contentStoreSQL, "content_entities_repo_idx") {
		t.Fatal("content_store SQL missing lookup index")
	}
	if strings.Contains(contentStoreSQL, "content_entities_source_trgm_idx") {
		t.Fatal("content_store SQL includes deferred entity search index")
	}
	if strings.Contains(contentStoreSQL, "content_files_content_trgm_idx") {
		t.Fatal("content_store SQL includes deferred file search index")
	}
}

func TestLocalHostChildOverridesDefaultsLogsToWorkspaceFile(t *testing.T) {
	layout := eshulocal.Layout{LogsDir: "/workspace/logs"}
	overrides := ChildOverrides(layout, map[string]string{"ESHU_REPOS_DIR": "/workspace/cache/repos"}, func(string) string {
		return ""
	})

	if got := overrides["ESHU_REPOS_DIR"]; got != "/workspace/cache/repos" {
		t.Fatalf("ESHU_REPOS_DIR = %q, want preserved override", got)
	}
	if got := overrides[LogModeEnv]; got != LogModeFile {
		t.Fatalf("%s = %q, want %q", LogModeEnv, got, LogModeFile)
	}
	if got := overrides[LogDirEnv]; got != layout.LogsDir {
		t.Fatalf("%s = %q, want %q", LogDirEnv, got, layout.LogsDir)
	}
}

func TestLocalHostChildOverridesPreservesExplicitLogMode(t *testing.T) {
	layout := eshulocal.Layout{LogsDir: "/workspace/logs"}
	overrides := ChildOverrides(layout, map[string]string{}, func(key string) string {
		if key == LogModeEnv {
			return LogModeTerminal
		}
		if key == LogDirEnv {
			return "/custom/logs"
		}
		return ""
	})

	if _, ok := overrides[LogModeEnv]; ok {
		t.Fatalf("%s override was set despite explicit environment", LogModeEnv)
	}
	if _, ok := overrides[LogDirEnv]; ok {
		t.Fatalf("%s override was set despite explicit environment", LogDirEnv)
	}
}

func TestLocalBootstrapDefinitionsIncludeContentSearchIndexesByDefault(t *testing.T) {
	defs := localBootstrapDefinitions(func(string) string { return "" })

	var contentStoreSQL string
	for _, def := range defs {
		if def.Name == "content_store" {
			contentStoreSQL = def.SQL
			break
		}
	}
	if contentStoreSQL == "" {
		t.Fatal("content_store definition missing")
	}
	if !strings.Contains(contentStoreSQL, "content_entities_source_trgm_idx") {
		t.Fatal("content_store SQL missing entity search index")
	}
	if !strings.Contains(contentStoreSQL, "content_files_content_trgm_idx") {
		t.Fatal("content_store SQL missing file search index")
	}
}

func TestRunAttachedLocalMCPStdioRejectsRequestedProfileMismatch(t *testing.T) {
	t.Setenv("ESHU_QUERY_PROFILE", string(query.ProfileLocalAuthoritative))

	layout := eshulocal.Layout{
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/tmp/owner.json",
	}
	record := eshulocal.OwnerRecord{
		PID:                42,
		WorkspaceID:        layout.WorkspaceID,
		PostgresPort:       15439,
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
		Profile:            string(query.ProfileLocalLightweight),
	}

	originalReadOwnerRecord := ReadOwnerRecord
	originalProcessAlive := ProcessAlive
	originalSocketHealthy := SocketHealthy
	originalStartChild := StartChildProcess
	t.Cleanup(func() {
		ReadOwnerRecord = originalReadOwnerRecord
		ProcessAlive = originalProcessAlive
		SocketHealthy = originalSocketHealthy
		StartChildProcess = originalStartChild
	})

	ReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	ProcessAlive = func(pid int) bool {
		return pid == record.PID
	}
	SocketHealthy = func(path string) bool {
		return path == record.PostgresSocketPath
	}

	startCalled := false
	StartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		startCalled = true
		return &exec.Cmd{}, nil
	}

	attached, err := RunAttachedMCPStdio(context.Background(), layout)
	if err == nil || !strings.Contains(err.Error(), "requested profile") {
		t.Fatalf("RunAttachedMCPStdio() error = %v, want profile mismatch error", err)
	}
	if !attached {
		t.Fatal("RunAttachedMCPStdio() attached = false, want true on owner mismatch failure")
	}
	if startCalled {
		t.Fatal("RunAttachedMCPStdio() started child process despite owner/profile mismatch")
	}
}

func TestRunAttachedLocalMCPStdioRejectsUnhealthyAuthoritativeGraph(t *testing.T) {
	layout := eshulocal.Layout{
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/tmp/owner.json",
	}
	record := eshulocal.OwnerRecord{
		PID:                42,
		WorkspaceID:        layout.WorkspaceID,
		PostgresPort:       15439,
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
		Profile:            string(query.ProfileLocalAuthoritative),
		GraphBackend:       string(query.GraphBackendNornicDB),
		GraphPID:           77,
		GraphBoltPort:      17687,
		GraphHTTPPort:      17474,
	}

	originalReadOwnerRecord := ReadOwnerRecord
	originalProcessAlive := ProcessAlive
	originalSocketHealthy := SocketHealthy
	originalGraphHealthy := GraphHealthy
	originalStartChild := StartChildProcess
	t.Cleanup(func() {
		ReadOwnerRecord = originalReadOwnerRecord
		ProcessAlive = originalProcessAlive
		SocketHealthy = originalSocketHealthy
		GraphHealthy = originalGraphHealthy
		StartChildProcess = originalStartChild
	})

	ReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	ProcessAlive = func(pid int) bool {
		return pid == record.PID
	}
	SocketHealthy = func(path string) bool {
		return path == record.PostgresSocketPath
	}
	GraphHealthy = func(record eshulocal.OwnerRecord) bool {
		return false
	}

	startCalled := false
	StartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		startCalled = true
		return &exec.Cmd{}, nil
	}

	attached, err := RunAttachedMCPStdio(context.Background(), layout)
	if err == nil || !strings.Contains(err.Error(), "graph backend") {
		t.Fatalf("RunAttachedMCPStdio() error = %v, want graph backend health error", err)
	}
	if !attached {
		t.Fatal("RunAttachedMCPStdio() attached = false, want true when owner exists but graph is unhealthy")
	}
	if startCalled {
		t.Fatal("RunAttachedMCPStdio() started MCP child despite unhealthy authoritative graph")
	}
}

func TestLocalHostIngesterOverridesUseFilesystemDirectMode(t *testing.T) {
	layout := eshulocal.Layout{
		WorkspaceRoot: "/workspace/repo",
		CacheDir:      "/eshu/cache",
	}

	got := localHostIngesterOverrides(layout, ModeWatch, RuntimeConfig{Profile: query.ProfileLocalLightweight}, func(string) string { return "" })
	if got["ESHU_REPO_SOURCE_MODE"] != "filesystem" {
		t.Fatalf("ESHU_REPO_SOURCE_MODE = %q, want %q", got["ESHU_REPO_SOURCE_MODE"], "filesystem")
	}
	if got["ESHU_FILESYSTEM_ROOT"] != layout.WorkspaceRoot {
		t.Fatalf("ESHU_FILESYSTEM_ROOT = %q, want %q", got["ESHU_FILESYSTEM_ROOT"], layout.WorkspaceRoot)
	}
	if got["ESHU_FILESYSTEM_DIRECT"] != "true" {
		t.Fatalf("ESHU_FILESYSTEM_DIRECT = %q, want %q", got["ESHU_FILESYSTEM_DIRECT"], "true")
	}
	wantReposDir := filepath.Join(layout.CacheDir, "repos")
	if got["ESHU_REPOS_DIR"] != wantReposDir {
		t.Fatalf("ESHU_REPOS_DIR = %q, want %q", got["ESHU_REPOS_DIR"], wantReposDir)
	}
}

func TestLocalHostEnvHonorsRuntimeConfig(t *testing.T) {
	t.Run("lightweight disables neo4j", func(t *testing.T) {
		got := ChildEnv("dsn", RuntimeConfig{Profile: query.ProfileLocalLightweight}, nil, nil)
		if envValue(got, "ESHU_QUERY_PROFILE") != string(query.ProfileLocalLightweight) {
			t.Fatalf("ESHU_QUERY_PROFILE = %q, want %q", envValue(got, "ESHU_QUERY_PROFILE"), query.ProfileLocalLightweight)
		}
		if envValue(got, "ESHU_DISABLE_NEO4J") != "true" {
			t.Fatalf("ESHU_DISABLE_NEO4J = %q, want %q", envValue(got, "ESHU_DISABLE_NEO4J"), "true")
		}
		if envValue(got, "ESHU_GRAPH_BACKEND") != "" {
			t.Fatalf("ESHU_GRAPH_BACKEND = %q, want empty", envValue(got, "ESHU_GRAPH_BACKEND"))
		}
	})

	t.Run("authoritative sets graph backend", func(t *testing.T) {
		originalEnviron := procexec.Environ
		procexec.Environ = func() []string {
			return []string{"ESHU_DISABLE_NEO4J=true"}
		}
		t.Cleanup(func() {
			procexec.Environ = originalEnviron
		})

		got := ChildEnv("dsn", RuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		}, nil, nil)
		if envValue(got, "ESHU_QUERY_PROFILE") != string(query.ProfileLocalAuthoritative) {
			t.Fatalf("ESHU_QUERY_PROFILE = %q, want %q", envValue(got, "ESHU_QUERY_PROFILE"), query.ProfileLocalAuthoritative)
		}
		if envValue(got, "ESHU_GRAPH_BACKEND") != string(query.GraphBackendNornicDB) {
			t.Fatalf("ESHU_GRAPH_BACKEND = %q, want %q", envValue(got, "ESHU_GRAPH_BACKEND"), query.GraphBackendNornicDB)
		}
		if envValue(got, "ESHU_DISABLE_NEO4J") != "" {
			t.Fatalf("ESHU_DISABLE_NEO4J = %q, want empty override", envValue(got, "ESHU_DISABLE_NEO4J"))
		}
	})

	t.Run("authoritative injects graph bolt connection", func(t *testing.T) {
		got := ChildEnv(
			"dsn",
			RuntimeConfig{
				Profile:      query.ProfileLocalAuthoritative,
				GraphBackend: query.GraphBackendNornicDB,
			},
			&ManagedGraph{
				Backend:  query.GraphBackendNornicDB,
				Address:  "127.0.0.1",
				BoltPort: 17687,
				Username: "admin",
				Password: "workspace-secret",
			},
			nil,
		)
		if envValue(got, "ESHU_NEO4J_URI") != "bolt://127.0.0.1:17687" {
			t.Fatalf("ESHU_NEO4J_URI = %q, want %q", envValue(got, "ESHU_NEO4J_URI"), "bolt://127.0.0.1:17687")
		}
		if envValue(got, "ESHU_NEO4J_USERNAME") != localNornicDBAdminUsername {
			t.Fatalf("ESHU_NEO4J_USERNAME = %q, want %q", envValue(got, "ESHU_NEO4J_USERNAME"), localNornicDBAdminUsername)
		}
		if envValue(got, "ESHU_NEO4J_PASSWORD") != "workspace-secret" {
			t.Fatalf("ESHU_NEO4J_PASSWORD = %q, want %q", envValue(got, "ESHU_NEO4J_PASSWORD"), "workspace-secret")
		}
		if envValue(got, "DEFAULT_DATABASE") != GraphDatabaseName {
			t.Fatalf("DEFAULT_DATABASE = %q, want %q", envValue(got, "DEFAULT_DATABASE"), GraphDatabaseName)
		}
	})
}

func TestRuntimeConfigFromOwnerRecordDefaultsAuthoritativeBackendToNornicDB(t *testing.T) {
	got, err := RuntimeConfigFromOwnerRecord(eshulocal.OwnerRecord{
		Profile: string(query.ProfileLocalAuthoritative),
	})
	if err != nil {
		t.Fatalf("RuntimeConfigFromOwnerRecord() error = %v, want nil", err)
	}
	if got.Profile != query.ProfileLocalAuthoritative {
		t.Fatalf("Profile = %q, want %q", got.Profile, query.ProfileLocalAuthoritative)
	}
	if got.GraphBackend != query.GraphBackendNornicDB {
		t.Fatalf("GraphBackend = %q, want %q", got.GraphBackend, query.GraphBackendNornicDB)
	}
}

func TestGraphHealthyFromOwnerRecord(t *testing.T) {
	originalProcessAlive := ProcessAlive
	originalGraphHTTPHealthy := localGraphHTTPHealthy
	originalGraphBoltHealthy := localGraphBoltHealthy
	t.Cleanup(func() {
		ProcessAlive = originalProcessAlive
		localGraphHTTPHealthy = originalGraphHTTPHealthy
		localGraphBoltHealthy = originalGraphBoltHealthy
	})

	record := eshulocal.OwnerRecord{
		GraphPID:      88,
		GraphAddress:  "127.0.0.1",
		GraphBoltPort: 17687,
		GraphHTTPPort: 17474,
	}
	ProcessAlive = func(pid int) bool {
		return pid == 88
	}
	localGraphHTTPHealthy = func(address string, port int, timeout time.Duration) bool {
		return address == "127.0.0.1" && port == 17474
	}
	localGraphBoltHealthy = func(address string, port int, timeout time.Duration) bool {
		return address == "127.0.0.1" && port == 17687
	}

	if !graphHealthyFromOwnerRecord(record) {
		t.Fatal("graphHealthyFromOwnerRecord() = false, want true")
	}
}

func TestWaitLocalChildProcessCancelUsesSingleWaiter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local child signal semantics are Unix-only in this slice")
	}

	cmd := exec.Command("/bin/sh", "-c", "trap 'exit 0' INT TERM; while :; do sleep 1; done")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- waitLocalChildProcess(ctx, cmd)
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waitLocalChildProcess() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitLocalChildProcess() timed out after cancel")
	}
}

func TestNormalizeLocalChildNaturalExitTreatsAlreadyWaitedAsClean(t *testing.T) {
	err := normalizeLocalChildNaturalExit(exec.ErrWaitDelay)
	if err == nil {
		t.Fatal("normalizeLocalChildNaturalExit(exec.ErrWaitDelay) = nil, want non-nil")
	}

	err = normalizeLocalChildNaturalExit(&exec.Error{Name: "child", Err: errors.New("Wait was already called")})
	if err != nil {
		t.Fatalf("normalizeLocalChildNaturalExit(already waited) error = %v, want nil", err)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
