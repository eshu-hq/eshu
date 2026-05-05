package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestGraphStatusForLayoutWithoutOwnerRecord(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalResolveBinary := graphResolveBinary
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphResolveBinary = originalResolveBinary
	})

	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return eshulocal.OwnerRecord{}, os.ErrNotExist
	}
	graphResolveBinary = func() (string, error) {
		return "", errors.New("not installed")
	}

	got, err := graphStatusForLayout(eshulocal.Layout{
		WorkspaceRoot:   "/workspace/repo",
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/workspace/owner.json",
	})
	if err != nil {
		t.Fatalf("graphStatusForLayout() error = %v, want nil", err)
	}
	if got.OwnerPresent {
		t.Fatal("OwnerPresent = true, want false")
	}
	if got.GraphRunning {
		t.Fatal("GraphRunning = true, want false")
	}
	if got.WorkspaceRoot != "/workspace/repo" {
		t.Fatalf("WorkspaceRoot = %q, want %q", got.WorkspaceRoot, "/workspace/repo")
	}
}

func TestGraphStatusForLayoutReportsRunningAuthoritativeBackend(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalResolveBinary := graphResolveBinary
	originalReadVersion := graphReadVersion
	originalProcessAlive := localHostProcessAlive
	originalGraphHTTPHealthy := localGraphHTTPHealthy
	originalGraphBoltHealthy := localGraphBoltHealthy
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphResolveBinary = originalResolveBinary
		graphReadVersion = originalReadVersion
		localHostProcessAlive = originalProcessAlive
		localGraphHTTPHealthy = originalGraphHTTPHealthy
		localGraphBoltHealthy = originalGraphBoltHealthy
	})

	record := eshulocal.OwnerRecord{
		PID:           100,
		StartedAt:     "2026-04-22T20:00:00Z",
		Profile:       string(query.ProfileLocalAuthoritative),
		GraphBackend:  string(query.GraphBackendNornicDB),
		GraphAddress:  "127.0.0.1",
		GraphPID:      200,
		GraphBoltPort: 17687,
		GraphHTTPPort: 17474,
		GraphDataDir:  "/workspace/graph/nornicdb",
		GraphVersion:  "1.0.42",
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphResolveBinary = func() (string, error) {
		return "/tmp/nornicdb", nil
	}
	graphReadVersion = func(binaryPath string) (string, error) {
		return "1.0.42", nil
	}
	localHostProcessAlive = func(pid int) bool {
		return pid == record.GraphPID
	}
	localGraphHTTPHealthy = func(address string, port int, timeout time.Duration) bool {
		return address == record.GraphAddress && port == record.GraphHTTPPort
	}
	localGraphBoltHealthy = func(address string, port int, timeout time.Duration) bool {
		return address == record.GraphAddress && port == record.GraphBoltPort
	}

	got, err := graphStatusForLayout(eshulocal.Layout{
		WorkspaceRoot:   "/workspace/repo",
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/workspace/owner.json",
	})
	if err != nil {
		t.Fatalf("graphStatusForLayout() error = %v, want nil", err)
	}
	if !got.OwnerPresent {
		t.Fatal("OwnerPresent = false, want true")
	}
	if got.Profile != string(query.ProfileLocalAuthoritative) {
		t.Fatalf("Profile = %q, want %q", got.Profile, query.ProfileLocalAuthoritative)
	}
	if got.GraphBackend != string(query.GraphBackendNornicDB) {
		t.Fatalf("GraphBackend = %q, want %q", got.GraphBackend, query.GraphBackendNornicDB)
	}
	if !got.GraphInstalled {
		t.Fatal("GraphInstalled = false, want true")
	}
	if got.GraphBinaryPath != "/tmp/nornicdb" {
		t.Fatalf("GraphBinaryPath = %q, want %q", got.GraphBinaryPath, "/tmp/nornicdb")
	}
	if !got.GraphRunning {
		t.Fatal("GraphRunning = false, want true")
	}
	if got.GraphBoltPort != 17687 {
		t.Fatalf("GraphBoltPort = %d, want %d", got.GraphBoltPort, 17687)
	}
	if got.GraphHTTPPort != 17474 {
		t.Fatalf("GraphHTTPPort = %d, want %d", got.GraphHTTPPort, 17474)
	}
}

func TestRunGraphStatusPrintsJSON(t *testing.T) {
	originalGetwd := graphGetwd
	originalBuildLayout := graphBuildLayout
	originalReadOwnerRecord := graphReadOwnerRecord
	t.Cleanup(func() {
		graphGetwd = originalGetwd
		graphBuildLayout = originalBuildLayout
		graphReadOwnerRecord = originalReadOwnerRecord
	})

	workspaceRoot := t.TempDir()
	graphGetwd = func() (string, error) {
		return workspaceRoot, nil
	}
	graphBuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
		return eshulocal.Layout{
			WorkspaceRoot:   workspaceRoot,
			WorkspaceID:     "workspace-id",
			OwnerRecordPath: "/workspace/owner.json",
		}, nil
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return eshulocal.OwnerRecord{}, os.ErrNotExist
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")

	output := captureStdout(t, func() {
		if err := runGraphStatus(cmd, nil); err != nil {
			t.Fatalf("runGraphStatus() error = %v, want nil", err)
		}
	})

	var got graphStatusOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("json.Unmarshal(output) error = %v, output=%q", err, output)
	}
	if got.WorkspaceID != "workspace-id" {
		t.Fatalf("WorkspaceID = %q, want %q", got.WorkspaceID, "workspace-id")
	}
}

func TestRunGraphLogsPrintsWorkspaceGraphLog(t *testing.T) {
	originalGetwd := graphGetwd
	originalBuildLayout := graphBuildLayout
	t.Cleanup(func() {
		graphGetwd = originalGetwd
		graphBuildLayout = originalBuildLayout
	})

	workspaceRoot := t.TempDir()
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(logsDir) error = %v, want nil", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "graph-nornicdb.log"), []byte("graph ready\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(graph log) error = %v, want nil", err)
	}
	graphGetwd = func() (string, error) {
		return workspaceRoot, nil
	}
	graphBuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
		return eshulocal.Layout{
			WorkspaceRoot: workspaceRoot,
			WorkspaceID:   "workspace-id",
			LogsDir:       logsDir,
		}, nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")

	output := captureStdout(t, func() {
		if err := runGraphLogs(cmd, nil); err != nil {
			t.Fatalf("runGraphLogs() error = %v, want nil", err)
		}
	})
	if output != "graph ready\n" {
		t.Fatalf("runGraphLogs() output = %q, want graph log content", output)
	}
}

func TestRunGraphLogsReturnsMissingLogGuidance(t *testing.T) {
	layout := eshulocal.Layout{LogsDir: t.TempDir()}

	err := graphLogsForLayout(layout)
	if err == nil {
		t.Fatal("graphLogsForLayout() error = nil, want missing log error")
	}
	if !strings.Contains(err.Error(), "graph log does not exist") {
		t.Fatalf("graphLogsForLayout() error = %q, want missing log guidance", err.Error())
	}
}

func TestRunGraphStartExecsAuthoritativeLocalHost(t *testing.T) {
	originalGetwd := graphGetwd
	originalBuildLayout := graphBuildLayout
	originalExecutable := eshuExecutable
	originalExec := eshuExec
	originalEnviron := eshuEnviron
	t.Cleanup(func() {
		graphGetwd = originalGetwd
		graphBuildLayout = originalBuildLayout
		eshuExecutable = originalExecutable
		eshuExec = originalExec
		eshuEnviron = originalEnviron
	})

	workspaceRoot := t.TempDir()
	graphGetwd = func() (string, error) {
		return workspaceRoot, nil
	}
	var resolvedWorkspaceRoot string
	graphBuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
		resolvedWorkspaceRoot = workspaceRoot
		return eshulocal.Layout{
			WorkspaceRoot: workspaceRoot,
			WorkspaceID:   "workspace-id",
		}, nil
	}
	eshuExecutable = func() (string, error) {
		return "/usr/local/bin/eshu", nil
	}
	eshuEnviron = func() []string {
		return []string{"ESHU_QUERY_PROFILE=local_lightweight"}
	}
	wantErr := errors.New("exec sentinel")
	var gotBinary string
	var gotArgs []string
	var gotEnv []string
	eshuExec = func(binary string, args []string, env []string) error {
		gotBinary = binary
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return wantErr
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")

	stderr := captureStderr(t, func() {
		err := runGraphStart(cmd, nil)
		if !errors.Is(err, wantErr) {
			t.Fatalf("runGraphStart() error = %v, want %v", err, wantErr)
		}
	})
	if !strings.Contains(stderr, "Starting local Eshu service") {
		t.Fatalf("runGraphStart() stderr = %q, want local Eshu service message", stderr)
	}
	if gotBinary != "/usr/local/bin/eshu" {
		t.Fatalf("exec binary = %q, want eshu path", gotBinary)
	}
	wantArgs := []string{"eshu", "local-host", "watch", resolvedWorkspaceRoot}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("exec args = %#v, want %#v", gotArgs, wantArgs)
	}
	if envValue(gotEnv, "ESHU_QUERY_PROFILE") != string(query.ProfileLocalAuthoritative) {
		t.Fatalf("ESHU_QUERY_PROFILE = %q, want local_authoritative", envValue(gotEnv, "ESHU_QUERY_PROFILE"))
	}
	if envValue(gotEnv, "ESHU_GRAPH_BACKEND") != string(query.GraphBackendNornicDB) {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want nornicdb", envValue(gotEnv, "ESHU_GRAPH_BACKEND"))
	}
}

func TestResolveNornicDBBinaryPrefersHeadlessBinary(t *testing.T) {
	originalLookPath := localGraphLookPath
	originalReadVersion := localGraphReadVersion
	t.Cleanup(func() {
		localGraphLookPath = originalLookPath
		localGraphReadVersion = originalReadVersion
	})
	t.Setenv("ESHU_HOME", t.TempDir())
	t.Setenv("ESHU_NORNICDB_BINARY", "")

	localGraphLookPath = func(file string) (string, error) {
		switch file {
		case "nornicdb-headless":
			return "/eshu/bin/nornicdb-headless", nil
		case "nornicdb":
			return "/eshu/bin/nornicdb", nil
		default:
			return "", errors.New("unexpected binary lookup")
		}
	}
	localGraphReadVersion = func(binaryPath string) (string, error) {
		return "v1.0.42", nil
	}

	got, err := resolveNornicDBBinary()
	if err != nil {
		t.Fatalf("resolveNornicDBBinary() error = %v, want nil", err)
	}
	if got != "/eshu/bin/nornicdb-headless" {
		t.Fatalf("resolveNornicDBBinary() = %q, want headless path", got)
	}
}

func TestResolveNornicDBBinaryAllowsExplicitFullBinary(t *testing.T) {
	originalReadVersion := localGraphReadVersion
	t.Cleanup(func() {
		localGraphReadVersion = originalReadVersion
	})
	t.Setenv("ESHU_NORNICDB_BINARY", "/opt/nornicdb")
	localGraphReadVersion = func(binaryPath string) (string, error) {
		return "v1.0.42", nil
	}

	got, err := resolveNornicDBBinary()
	if err != nil {
		t.Fatalf("resolveNornicDBBinary() error = %v, want nil", err)
	}
	if got != "/opt/nornicdb" {
		t.Fatalf("resolveNornicDBBinary() = %q, want explicit path", got)
	}
}

func TestResolveNornicDBBinaryRejectsInvalidExplicitBinary(t *testing.T) {
	originalReadVersion := localGraphReadVersion
	t.Cleanup(func() {
		localGraphReadVersion = originalReadVersion
	})
	t.Setenv("ESHU_NORNICDB_BINARY", "/tmp/not-nornicdb")
	localGraphReadVersion = func(binaryPath string) (string, error) {
		return "", errors.New("unexpected output")
	}

	_, err := resolveNornicDBBinary()
	if err == nil {
		t.Fatal("resolveNornicDBBinary() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "verify nornicdb binary") {
		t.Fatalf("resolveNornicDBBinary() error = %q, want verification failure", err.Error())
	}
}

func TestParseNornicDBVersionOutputRequiresNornicDBPrefix(t *testing.T) {
	got, err := parseNornicDBVersionOutput("NornicDB v1.0.42\n")
	if err != nil {
		t.Fatalf("parseNornicDBVersionOutput() error = %v, want nil", err)
	}
	if got != "v1.0.42" {
		t.Fatalf("parseNornicDBVersionOutput() = %q, want %q", got, "v1.0.42")
	}

	_, err = parseNornicDBVersionOutput("not nornicdb\n")
	if err == nil {
		t.Fatal("parseNornicDBVersionOutput() error = nil, want non-nil")
	}
}

func TestLoadOrCreateLocalGraphCredentialsReusesWorkspaceSecret(t *testing.T) {
	originalGeneratePassword := localGraphGeneratePassword
	t.Cleanup(func() {
		localGraphGeneratePassword = originalGeneratePassword
	})

	credentialPath := filepath.Join(t.TempDir(), "graph", "nornicdb", "eshu-credentials.json")
	generated := 0
	localGraphGeneratePassword = func() (string, error) {
		generated++
		return "workspace-secret", nil
	}

	first, err := loadOrCreateLocalGraphCredentials(credentialPath)
	if err != nil {
		t.Fatalf("loadOrCreateLocalGraphCredentials() error = %v, want nil", err)
	}
	if first.Username != localNornicDBAdminUsername || first.Password != "workspace-secret" {
		t.Fatalf("credentials = %+v, want generated admin secret", first)
	}
	info, err := os.Stat(credentialPath)
	if err != nil {
		t.Fatalf("os.Stat(credentials) error = %v, want nil", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode = %v, want 0600", info.Mode().Perm())
	}

	localGraphGeneratePassword = func() (string, error) {
		generated++
		return "rotated-secret", nil
	}
	second, err := loadOrCreateLocalGraphCredentials(credentialPath)
	if err != nil {
		t.Fatalf("second loadOrCreateLocalGraphCredentials() error = %v, want nil", err)
	}
	if second.Password != "workspace-secret" {
		t.Fatalf("second password = %q, want persisted workspace secret", second.Password)
	}
	if generated != 1 {
		t.Fatalf("password generated %d times, want 1", generated)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v, want nil", err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
	})

	done := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = io.Copy(&buffer, reader)
		done <- buffer.String()
	}()

	fn()

	_ = writer.Close()
	got := <-done
	return got
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v, want nil", err)
	}
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = originalStderr
	})

	done := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = io.Copy(&buffer, reader)
		done <- buffer.String()
	}()

	fn()

	_ = writer.Close()
	got := <-done
	return got
}

func TestRunGraphStatusReturnsBuildLayoutError(t *testing.T) {
	originalGetwd := graphGetwd
	originalBuildLayout := graphBuildLayout
	t.Cleanup(func() {
		graphGetwd = originalGetwd
		graphBuildLayout = originalBuildLayout
	})

	workspaceRoot := t.TempDir()
	graphGetwd = func() (string, error) {
		return workspaceRoot, nil
	}
	graphBuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
		return eshulocal.Layout{}, errors.New("layout failed")
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")

	err := runGraphStatus(cmd, nil)
	if err == nil || err.Error() != "layout failed" {
		t.Fatalf("runGraphStatus() error = %v, want %q", err, "layout failed")
	}
}
