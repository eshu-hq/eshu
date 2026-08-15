// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	got, err := StatusForLayout(eshulocal.Layout{
		WorkspaceRoot:   "/workspace/repo",
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/workspace/owner.json",
	})
	if err != nil {
		t.Fatalf("StatusForLayout() error = %v, want nil", err)
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
	originalProcessAlive := ProcessAlive
	originalGraphHTTPHealthy := localGraphHTTPHealthy
	originalGraphBoltHealthy := localGraphBoltHealthy
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphResolveBinary = originalResolveBinary
		graphReadVersion = originalReadVersion
		ProcessAlive = originalProcessAlive
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
	ProcessAlive = func(pid int) bool {
		return pid == record.GraphPID
	}
	localGraphHTTPHealthy = func(address string, port int, timeout time.Duration) bool {
		return address == record.GraphAddress && port == record.GraphHTTPPort
	}
	localGraphBoltHealthy = func(address string, port int, timeout time.Duration) bool {
		return address == record.GraphAddress && port == record.GraphBoltPort
	}

	got, err := StatusForLayout(eshulocal.Layout{
		WorkspaceRoot:   "/workspace/repo",
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/workspace/owner.json",
	})
	if err != nil {
		t.Fatalf("StatusForLayout() error = %v, want nil", err)
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

func TestRunGraphLogsReturnsMissingLogGuidance(t *testing.T) {
	layout := eshulocal.Layout{LogsDir: t.TempDir()}

	err := LogsForLayout(layout, nil)
	if err == nil {
		t.Fatal("LogsForLayout() error = nil, want missing log error")
	}
	if !strings.Contains(err.Error(), "graph log does not exist") {
		t.Fatalf("LogsForLayout() error = %q, want missing log guidance", err.Error())
	}
}

func TestResolveNornicDBBinaryPrefersHeadlessBinary(t *testing.T) {
	originalLookPath := localGraphLookPath
	originalReadVersion := ReadGraphVersion
	t.Cleanup(func() {
		localGraphLookPath = originalLookPath
		ReadGraphVersion = originalReadVersion
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
	ReadGraphVersion = func(binaryPath string) (string, error) {
		return "v1.0.42", nil
	}

	got, err := ResolveGraphBinary()
	if err != nil {
		t.Fatalf("ResolveGraphBinary() error = %v, want nil", err)
	}
	if got != "/eshu/bin/nornicdb-headless" {
		t.Fatalf("ResolveGraphBinary() = %q, want headless path", got)
	}
}

func TestResolveNornicDBBinaryAllowsExplicitFullBinary(t *testing.T) {
	originalReadVersion := ReadGraphVersion
	t.Cleanup(func() {
		ReadGraphVersion = originalReadVersion
	})
	t.Setenv("ESHU_NORNICDB_BINARY", "/opt/nornicdb")
	ReadGraphVersion = func(binaryPath string) (string, error) {
		return "v1.0.42", nil
	}

	got, err := ResolveGraphBinary()
	if err != nil {
		t.Fatalf("ResolveGraphBinary() error = %v, want nil", err)
	}
	if got != "/opt/nornicdb" {
		t.Fatalf("ResolveGraphBinary() = %q, want explicit path", got)
	}
}

func TestResolveNornicDBBinaryRejectsInvalidExplicitBinary(t *testing.T) {
	originalReadVersion := ReadGraphVersion
	t.Cleanup(func() {
		ReadGraphVersion = originalReadVersion
	})
	t.Setenv("ESHU_NORNICDB_BINARY", "/tmp/not-nornicdb")
	ReadGraphVersion = func(binaryPath string) (string, error) {
		return "", errors.New("unexpected output")
	}

	_, err := ResolveGraphBinary()
	if err == nil {
		t.Fatal("ResolveGraphBinary() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "verify nornicdb binary") {
		t.Fatalf("ResolveGraphBinary() error = %q, want verification failure", err.Error())
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
