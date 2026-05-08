package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

func TestRunMCPStartStdioExecsLocalHostWithResolvedWorkspaceRoot(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	startPath := filepath.Join(repoRoot, "pkg")
	if err := os.MkdirAll(startPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.getwd = func() (string, error) { return startPath, nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		calls.env = append([]string(nil), env...)
		return wantExecErr
	}

	cmd := newMCPStartTestCommand()
	err := runMCPStart(cmd, nil)
	if !errors.Is(err, wantExecErr) {
		t.Fatalf("runMCPStart() error = %v, want %v", err, wantExecErr)
	}

	if got, want := calls.binary, "/tmp/eshu"; got != want {
		t.Fatalf("exec binary = %q, want %q", got, want)
	}
	wantWorkspaceRoot := mustEvalSymlinks(t, repoRoot)
	wantArgs := []string{"eshu", "local-host", "mcp-stdio", wantWorkspaceRoot}
	if !reflect.DeepEqual(calls.args, wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", calls.args, wantArgs)
	}
}

func TestRunMCPStartSSEAttachesRunningWorkspaceOwner(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()
	restoreOwner := stubLocalOwnerForMCP(t, repoRoot, eshulocal.OwnerRecord{
		PID:                1234,
		WorkspaceID:        "workspace-123",
		PostgresPort:       15439,
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
		Profile:            "local_authoritative",
		GraphBackend:       "nornicdb",
		GraphAddress:       "127.0.0.1",
		GraphBoltPort:      17687,
		GraphUsername:      "admin",
		GraphPassword:      "workspace-secret",
	})
	defer restoreOwner()

	wantExecErr := errors.New("exec sentinel")
	calls.getwd = func() (string, error) { return repoRoot, nil }
	calls.lookPath = func(binary string) (string, error) {
		if binary != "eshu-mcp-server" {
			t.Fatalf("LookPath(%q), want eshu-mcp-server", binary)
		}
		return "/tmp/eshu-mcp-server", nil
	}
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		calls.env = append([]string(nil), env...)
		return wantExecErr
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("transport", "sse"); err != nil {
		t.Fatalf("Set(transport) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("host", "127.0.0.1"); err != nil {
		t.Fatalf("Set(host) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("port", "18191"); err != nil {
		t.Fatalf("Set(port) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("workspace-root", repoRoot); err != nil {
		t.Fatalf("Set(workspace-root) error = %v, want nil", err)
	}

	err := runMCPStart(cmd, nil)
	if !errors.Is(err, wantExecErr) {
		t.Fatalf("runMCPStart() error = %v, want %v", err, wantExecErr)
	}

	if got, want := calls.binary, "/tmp/eshu-mcp-server"; got != want {
		t.Fatalf("exec binary = %q, want %q", got, want)
	}
	if got, want := calls.args, []string{"eshu-mcp-server"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("exec args = %#v, want %#v", got, want)
	}
	assertEnvValue(t, calls.env, "ESHU_MCP_TRANSPORT", "http")
	assertEnvValue(t, calls.env, "ESHU_MCP_ADDR", "127.0.0.1:18191")
	assertEnvValue(t, calls.env, "ESHU_QUERY_PROFILE", "local_authoritative")
	assertEnvValue(t, calls.env, "ESHU_GRAPH_BACKEND", "nornicdb")
	assertEnvValue(t, calls.env, "ESHU_NEO4J_URI", "bolt://127.0.0.1:17687")
	assertEnvValue(t, calls.env, "ESHU_NEO4J_USERNAME", "admin")
	assertEnvValue(t, calls.env, "ESHU_NEO4J_PASSWORD", "workspace-secret")
	assertEnvValue(t, calls.env, "ESHU_POSTGRES_DSN", "host=127.0.0.1 port=15439 user=eshu password=change-me dbname=postgres sslmode=disable")
}

func TestRunMCPStartSSEWithWorkspaceRootRequiresRunningOwner(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()
	restoreOwner := stubLocalOwnerForMCP(t, repoRoot, eshulocal.OwnerRecord{})
	defer restoreOwner()

	calls.getwd = func() (string, error) { return repoRoot, nil }
	calls.lookPath = func(string) (string, error) { return "/tmp/eshu-mcp-server", nil }
	calls.exec = func(string, []string, []string) error {
		t.Fatalf("exec called, want owner preflight error")
		return nil
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("transport", "sse"); err != nil {
		t.Fatalf("Set(transport) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("workspace-root", repoRoot); err != nil {
		t.Fatalf("Set(workspace-root) error = %v, want nil", err)
	}

	err := runMCPStart(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "no running local Eshu service owner") {
		t.Fatalf("runMCPStart() error = %v, want missing owner error", err)
	}
}

type serviceRuntimeCalls struct {
	executable func() (string, error)
	getwd      func() (string, error)
	lookPath   func(string) (string, error)
	exec       func(string, []string, []string) error
	binary     string
	args       []string
	env        []string
}

func stubServiceRuntime() (func(), *serviceRuntimeCalls) {
	calls := &serviceRuntimeCalls{}

	originalExecutable := eshuExecutable
	originalGetwd := eshuGetwd
	originalLookPath := eshuLookPath
	originalExec := eshuExec
	originalEnviron := eshuEnviron

	eshuExecutable = func() (string, error) {
		if calls.executable == nil {
			return "", errors.New("eshuExecutable not stubbed")
		}
		return calls.executable()
	}
	eshuGetwd = func() (string, error) {
		if calls.getwd == nil {
			return "", errors.New("eshuGetwd not stubbed")
		}
		return calls.getwd()
	}
	eshuLookPath = func(binary string) (string, error) {
		if calls.lookPath == nil {
			return "", errors.New("eshuLookPath not stubbed")
		}
		return calls.lookPath(binary)
	}
	eshuExec = func(binary string, args []string, env []string) error {
		if calls.exec == nil {
			return errors.New("eshuExec not stubbed")
		}
		return calls.exec(binary, args, env)
	}
	eshuEnviron = func() []string {
		return []string{"PATH=/tmp"}
	}

	return func() {
		eshuExecutable = originalExecutable
		eshuGetwd = originalGetwd
		eshuLookPath = originalLookPath
		eshuExec = originalExec
		eshuEnviron = originalEnviron
	}, calls
}

func stubLocalOwnerForMCP(t *testing.T, repoRoot string, record eshulocal.OwnerRecord) func() {
	t.Helper()

	originalBuildLayout := localHostBuildLayout
	originalReadOwnerRecord := localHostReadOwnerRecord
	originalProcessAlive := localHostProcessAlive
	originalSocketHealthy := localHostSocketHealthy
	originalGraphHealthy := localHostGraphHealthy

	workspaceRoot := mustEvalSymlinks(t, repoRoot)
	localHostBuildLayout = func(root string) (eshulocal.Layout, error) {
		if got := mustEvalSymlinks(t, root); got != workspaceRoot {
			t.Fatalf("BuildLayout(%q) resolved to %q, want %q", root, got, workspaceRoot)
		}
		return eshulocal.Layout{
			WorkspaceRoot:   workspaceRoot,
			WorkspaceID:     "workspace-123",
			OwnerRecordPath: filepath.Join(t.TempDir(), "owner.json"),
		}, nil
	}
	localHostReadOwnerRecord = func(string) (eshulocal.OwnerRecord, error) {
		if record.PID == 0 {
			return eshulocal.OwnerRecord{}, os.ErrNotExist
		}
		return record, nil
	}
	localHostProcessAlive = func(int) bool { return record.PID != 0 }
	localHostSocketHealthy = func(string) bool { return record.PID != 0 }
	localHostGraphHealthy = func(eshulocal.OwnerRecord) bool { return record.PID != 0 }

	return func() {
		localHostBuildLayout = originalBuildLayout
		localHostReadOwnerRecord = originalReadOwnerRecord
		localHostProcessAlive = originalProcessAlive
		localHostSocketHealthy = originalSocketHealthy
		localHostGraphHealthy = originalGraphHealthy
	}
}

func assertEnvValue(t *testing.T, env []string, key, want string) {
	t.Helper()
	if got := envValue(env, key); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func newMCPStartTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("transport", "stdio", "")
	cmd.Flags().String("host", "127.0.0.1", "")
	cmd.Flags().Int("port", 0, "")
	cmd.Flags().String("workspace-root", "", "")
	return cmd
}
