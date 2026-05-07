//go:build !windows

package eshulocal

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

func TestPostgresDSNUsesLoopbackTCP(t *testing.T) {
	t.Parallel()

	got := PostgresDSN("127.0.0.1", 15439)
	want := "host=127.0.0.1 port=15439 user=eshu password=change-me dbname=postgres sslmode=disable"
	if got != want {
		t.Fatalf("PostgresDSN() = %q, want %q", got, want)
	}
}

func TestRuntimeSocketDirFallsBackWhenTempDirPathIsTooLong(t *testing.T) {
	t.Parallel()

	layout := Layout{WorkspaceID: strings.Repeat("a", 40)}
	baseTempDir := "/var/folders/__/fmq5zy6978g8g9y_jdqf1mbh0000gp/T"

	got := runtimeSocketDir(layout, baseTempDir)
	wantPrefix := filepath.Join("/tmp", "eshu")
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("runtimeSocketDir() = %q, want prefix %q", got, wantPrefix)
	}
	if got == filepath.Join(baseTempDir, "eshu", layout.WorkspaceID) {
		t.Fatalf("runtimeSocketDir() = %q, want fallback away from long tmpdir path", got)
	}
}

func TestEmbeddedPostgresConfigRoutesStartupLogsToWorkspaceFile(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	config := embeddedPostgresConfig(
		"/workspace/postgres/data",
		"/workspace/postgres/runtime",
		"/workspace/postgres/binaries",
		"/workspace/cache/embedded-postgres",
		"/tmp/eshu/workspace",
		15439,
		&logs,
	)
	runtime := embeddedpostgres.NewDatabase(config)
	if runtime == nil {
		t.Fatal("NewDatabase() = nil, want runtime from configured logger")
	}
	if _, err := logs.WriteString("captured"); err != nil {
		t.Fatalf("log writer WriteString() error = %v, want nil", err)
	}
	if got := logs.String(); got != "captured" {
		t.Fatalf("log writer = %q, want captured startup output", got)
	}
}
