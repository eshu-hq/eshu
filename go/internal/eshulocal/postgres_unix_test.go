// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !windows

package eshulocal

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
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

// TestLocalPostgresPoolHoldersCoverEveryLocalProcessThatOpensAPool pins the
// enumerated holder list against the processes that actually open a pool on
// this server.
//
// This is the assertion that carries the budget. The ceiling is DEFINED as
// len(holders)*perProcess+reserved, so a test comparing the ceiling to that
// product proves nothing on its own -- it is the same expression twice. What
// can go wrong is the list falling behind the code that starts these
// processes, or someone shortening it to make a budget complaint go away. This
// test fails in both cases.
//
// Sources for the list, to check it against when a process is added:
//   - internal/cli/localsupervisor/host.go starts eshu-reducer, eshu-ingester
//     and eshu-mcp-server as supervised children.
//   - cmd/eshu/gotchas-read-surface-commands.md documents `eshu vuln-scan repo`
//     attaching to a running owner and launching eshu-api plus
//     eshu-bootstrap-index against that owner's Postgres.
func TestLocalPostgresPoolHoldersCoverEveryLocalProcessThatOpensAPool(t *testing.T) {
	t.Parallel()

	want := []string{
		"eshu-reducer",
		"eshu-ingester",
		"eshu-mcp-server",
		"eshu-api",
		"eshu-bootstrap-index",
	}
	got := make(map[string]bool, len(localPostgresPoolHolders))
	for _, holder := range localPostgresPoolHolders {
		got[holder] = true
	}
	for _, holder := range want {
		if !got[holder] {
			t.Fatalf("localPostgresPoolHolders is missing %q; it holds %v. Every process that opens a pool against the embedded server must be listed, because the ceiling is derived from len()",
				holder, localPostgresPoolHolders)
		}
	}
	if len(localPostgresPoolHolders) != len(want) {
		t.Fatalf("localPostgresPoolHolders = %v (%d entries), want the %d known holders; add the new holder to `want` here and to the budget note in docs/public/reference/postgres-tuning.md",
			localPostgresPoolHolders, len(localPostgresPoolHolders), len(want))
	}
}

// TestEmbeddedPostgresMaxConnectionsCoversLocalPoolBudget extends the #4456
// connection-budget invariant to the embedded local server.
//
// internal/runtime enforces this for the Compose stacks
// (TestComposePostgresMaxConnectionsCoversPoolBudget). The embedded server was
// exempt and carried a bare "35" literal, which is below the budget for even a
// single pool-holding process (30 + 20 reserved = 50).
//
// What this test can and cannot prove, stated plainly: the ceiling is derived
// from the same three figures it is compared against, so this catches a literal
// substituted for the derivation and nothing subtler. The list itself is pinned
// by TestLocalPostgresPoolHoldersCoverEveryLocalProcessThatOpensAPool above,
// which is where the real coverage lives.
func TestEmbeddedPostgresMaxConnectionsCoversLocalPoolBudget(t *testing.T) {
	t.Parallel()

	peakDemand := len(localPostgresPoolHolders) * localPostgresPerProcessPoolConns
	want := peakDemand + localPostgresReservedConns

	if LocalPostgresMaxConnections < want {
		t.Fatalf("LocalPostgresMaxConnections = %d, want >= %d (%d pool-holding services * %d per-process pool + %d reserved/admin)",
			LocalPostgresMaxConnections, want,
			len(localPostgresPoolHolders), localPostgresPerProcessPoolConns, localPostgresReservedConns)
	}
}

// TestEmbeddedPostgresConfigCarriesDerivedMaxConnections pins the start
// parameters to the derived constant.
//
// It reads this package's own source because embeddedpostgres.Config keeps
// startParameters unexported with no getter, so the value cannot be read back
// off the config. That makes this a weak check by construction: it proves the
// call site still spells the constant, not that the server honours it. The
// running server is covered by
// TestStartEmbeddedPostgresBootstrapsThroughForkedDriverLive, which asserts
// SHOW max_connections against a real postmaster.
func TestEmbeddedPostgresConfigCarriesDerivedMaxConnections(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("postgres_unix.go")
	if err != nil {
		t.Fatalf("ReadFile(postgres_unix.go) error = %v, want nil", err)
	}
	want := `"max_connections":         strconv.Itoa(ResolveLocalPostgresMaxConnections(os.Getenv)),`
	if !strings.Contains(string(source), want) {
		t.Fatalf("postgres_unix.go start parameters do not carry %s; max_connections must stay derived from the budget, not a literal", want)
	}
}

// TestResolveLocalPostgresMaxConnectionsFollowsConfiguredPool covers the gap a
// compile-time ceiling cannot: localsupervisor.ChildEnv passes
// ESHU_POSTGRES_MAX_OPEN_CONNS through to every child untouched, so an operator
// who raises the documented knob raises each child's pool while a fixed ceiling
// stays put. postgres-tuning.md states the invariant as
// sum(pool-holding services * ESHU_POSTGRES_MAX_OPEN_CONNS); this asserts the
// embedded server actually satisfies it for a configured pool rather than only
// for the default (#6603 review).
func TestResolveLocalPostgresMaxConnectionsFollowsConfiguredPool(t *testing.T) {
	t.Parallel()

	holders := len(localPostgresPoolHolders)
	for _, tc := range []struct {
		name string
		set  string
		want int
	}{
		{name: "unset uses the derived floor", set: "", want: LocalPostgresMaxConnections},
		{name: "default restates the floor", set: "30", want: LocalPostgresMaxConnections},
		{name: "raised pool raises the ceiling", set: "60", want: holders*60 + localPostgresReservedConns},
		{name: "large pool raises the ceiling", set: "100", want: holders*100 + localPostgresReservedConns},
		{name: "lowered pool cannot lower the floor", set: "5", want: LocalPostgresMaxConnections},
		{name: "zero is not honoured", set: "0", want: LocalPostgresMaxConnections},
		{name: "negative is not honoured", set: "-8", want: LocalPostgresMaxConnections},
		{name: "unparseable is not honoured", set: "sixty", want: LocalPostgresMaxConnections},
		{name: "whitespace is not a value", set: "   ", want: LocalPostgresMaxConnections},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				if key != localPostgresMaxOpenConnsEnv {
					t.Fatalf("read unexpected env key %q, want only %q", key, localPostgresMaxOpenConnsEnv)
				}
				return tc.set
			}
			if got := ResolveLocalPostgresMaxConnections(getenv); got != tc.want {
				t.Fatalf("ResolveLocalPostgresMaxConnections(%q) = %d, want %d", tc.set, got, tc.want)
			}
		})
	}
}

// TestResolveLocalPostgresMaxConnectionsCoversTheConfiguredPoolBudget states the
// invariant itself rather than the arithmetic, so it still fails if the formula
// is changed to something that happens to match the cases above.
func TestResolveLocalPostgresMaxConnectionsCoversTheConfiguredPoolBudget(t *testing.T) {
	t.Parallel()

	for _, perProcess := range []int{1, 30, 45, 60, 100, 250} {
		getenv := func(string) string { return strconv.Itoa(perProcess) }
		got := ResolveLocalPostgresMaxConnections(getenv)
		demand := len(localPostgresPoolHolders)*perProcess + localPostgresReservedConns
		if got < demand {
			t.Fatalf("ESHU_POSTGRES_MAX_OPEN_CONNS=%d: ceiling %d is below the pool budget %d (%d holders * %d + %d reserved)",
				perProcess, got, demand, len(localPostgresPoolHolders), perProcess, localPostgresReservedConns)
		}
		if got < LocalPostgresMaxConnections {
			t.Fatalf("ESHU_POSTGRES_MAX_OPEN_CONNS=%d: ceiling %d fell below the documented floor %d",
				perProcess, got, LocalPostgresMaxConnections)
		}
	}
}

// TestResolveLocalPostgresMaxConnectionsNilGetenvUsesFloor pins the nil case,
// which the config path does not take but a caller could.
func TestResolveLocalPostgresMaxConnectionsNilGetenvUsesFloor(t *testing.T) {
	t.Parallel()

	if got := ResolveLocalPostgresMaxConnections(nil); got != LocalPostgresMaxConnections {
		t.Fatalf("ResolveLocalPostgresMaxConnections(nil) = %d, want %d", got, LocalPostgresMaxConnections)
	}
}
