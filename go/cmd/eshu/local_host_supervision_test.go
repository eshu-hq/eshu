package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestRunOwnedLocalHostWithLayoutAuthoritativeWatchStartsReducerAndIngester(t *testing.T) {
	t.Setenv("ESHU_QUERY_PROFILE", string(query.ProfileLocalAuthoritative))

	originalPrepareWorkspace := localHostPrepareWorkspace
	originalStartEmbeddedPostgres := localHostStartEmbeddedPostgres
	originalStartManagedGraph := localHostStartManagedGraph
	originalWriteOwnerRecord := localHostWriteOwnerRecord
	originalHostname := localHostHostname
	originalStartChild := localHostStartChildProcess
	originalWaitManagedChildren := localHostWaitManagedChildren
	originalWaitOwnerChildren := localHostWaitOwnerChildren
	originalApplyBootstrap := localHostApplyBootstrap
	originalApplyGraphBootstrap := localHostApplyGraphBootstrap
	originalExpectedProjectors := localHostContentSearchIndexExpectedProjectors
	originalStartIaCReachabilityFinalizer := localHostStartIaCReachabilityFinalizer
	t.Cleanup(func() {
		localHostPrepareWorkspace = originalPrepareWorkspace
		localHostStartEmbeddedPostgres = originalStartEmbeddedPostgres
		localHostStartManagedGraph = originalStartManagedGraph
		localHostWriteOwnerRecord = originalWriteOwnerRecord
		localHostHostname = originalHostname
		localHostStartChildProcess = originalStartChild
		localHostWaitManagedChildren = originalWaitManagedChildren
		localHostWaitOwnerChildren = originalWaitOwnerChildren
		localHostApplyBootstrap = originalApplyBootstrap
		localHostApplyGraphBootstrap = originalApplyGraphBootstrap
		localHostContentSearchIndexExpectedProjectors = originalExpectedProjectors
		localHostStartIaCReachabilityFinalizer = originalStartIaCReachabilityFinalizer
	})

	localHostPrepareWorkspace = func(layout eshulocal.Layout) (*eshulocal.OwnerLock, error) {
		return &eshulocal.OwnerLock{}, nil
	}
	localHostStartEmbeddedPostgres = func(ctx context.Context, layout eshulocal.Layout) (*eshulocal.ManagedPostgres, error) {
		return &eshulocal.ManagedPostgres{
			DSN:        "host=127.0.0.1 port=15439 user=eshu password=change-me dbname=postgres sslmode=disable",
			Port:       15439,
			DataDir:    "/workspace/postgres/data",
			SocketDir:  "/tmp/eshu",
			SocketPath: "/tmp/eshu/.s.PGSQL.15439",
			PID:        21,
		}, nil
	}
	localHostStartManagedGraph = func(ctx context.Context, layout eshulocal.Layout, runtimeConfig localHostRuntimeConfig) (*managedLocalGraph, error) {
		return &managedLocalGraph{
			Backend:  query.GraphBackendNornicDB,
			Address:  "127.0.0.1",
			BoltPort: 17687,
			HTTPPort: 17474,
			Username: "admin",
			Password: "workspace-secret",
			PID:      88,
			Cmd:      &exec.Cmd{},
		}, nil
	}
	localHostWriteOwnerRecord = func(path string, record eshulocal.OwnerRecord) error {
		return nil
	}
	localHostHostname = func() (string, error) {
		return "local-test", nil
	}
	localHostApplyBootstrap = func(ctx context.Context, dsn string) error {
		return nil
	}
	localHostApplyGraphBootstrap = func(ctx context.Context, runtimeConfig localHostRuntimeConfig, graph *managedLocalGraph) error {
		return nil
	}
	localHostContentSearchIndexExpectedProjectors = func(workspaceRoot string) (int, error) {
		return 1, nil
	}
	localHostStartIaCReachabilityFinalizer = func(ctx context.Context, dsn string, expectedProjectors int) (func() error, error) {
		return func() error { return nil }, nil
	}

	var started []string
	localHostStartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		started = append(started, name)
		return &exec.Cmd{}, nil
	}

	var waited []string
	var allowedCleanExit bool
	localHostWaitOwnerChildren = func(ctx context.Context, children []localHostChild, allowedCleanExits map[string]struct{}) error {
		for _, child := range children {
			waited = append(waited, child.name)
		}
		_, allowedCleanExit = allowedCleanExits["eshu-ingester"]
		return nil
	}

	err := runOwnedLocalHostWithLayout(context.Background(), eshulocal.Layout{
		WorkspaceID:     "workspace-id",
		WorkspaceRoot:   "/workspace/repo",
		OwnerRecordPath: "/workspace/owner.json",
		CacheDir:        "/workspace/cache",
		LogsDir:         "/workspace/logs",
		GraphDir:        "/workspace/graph",
	}, localHostModeWatch)
	if err != nil {
		t.Fatalf("runOwnedLocalHostWithLayout() error = %v, want nil", err)
	}

	wantChildren := []string{"eshu-reducer", "eshu-ingester"}
	if !slices.Equal(started, wantChildren) {
		t.Fatalf("started children = %#v, want %#v", started, wantChildren)
	}
	if !slices.Equal(waited, wantChildren) {
		t.Fatalf("waited children = %#v, want %#v", waited, wantChildren)
	}
	if !allowedCleanExit {
		t.Fatal("allowedCleanExits missing eshu-ingester")
	}
}

func TestRunOwnedLocalHostWithLayoutLightweightWatchStartsOnlyIngester(t *testing.T) {
	originalPrepareWorkspace := localHostPrepareWorkspace
	originalStartEmbeddedPostgres := localHostStartEmbeddedPostgres
	originalWriteOwnerRecord := localHostWriteOwnerRecord
	originalHostname := localHostHostname
	originalStartChild := localHostStartChildProcess
	originalWaitManagedChildren := localHostWaitManagedChildren
	originalApplyBootstrap := localHostApplyBootstrap
	t.Cleanup(func() {
		localHostPrepareWorkspace = originalPrepareWorkspace
		localHostStartEmbeddedPostgres = originalStartEmbeddedPostgres
		localHostWriteOwnerRecord = originalWriteOwnerRecord
		localHostHostname = originalHostname
		localHostStartChildProcess = originalStartChild
		localHostWaitManagedChildren = originalWaitManagedChildren
		localHostApplyBootstrap = originalApplyBootstrap
	})

	localHostPrepareWorkspace = func(layout eshulocal.Layout) (*eshulocal.OwnerLock, error) {
		return &eshulocal.OwnerLock{}, nil
	}
	localHostStartEmbeddedPostgres = func(ctx context.Context, layout eshulocal.Layout) (*eshulocal.ManagedPostgres, error) {
		return &eshulocal.ManagedPostgres{
			DSN:        "host=127.0.0.1 port=15439 user=eshu password=change-me dbname=postgres sslmode=disable",
			Port:       15439,
			DataDir:    "/workspace/postgres/data",
			SocketDir:  "/tmp/eshu",
			SocketPath: "/tmp/eshu/.s.PGSQL.15439",
			PID:        21,
		}, nil
	}
	localHostWriteOwnerRecord = func(path string, record eshulocal.OwnerRecord) error {
		return nil
	}
	localHostHostname = func() (string, error) {
		return "local-test", nil
	}
	localHostApplyBootstrap = func(ctx context.Context, dsn string) error {
		return nil
	}

	var started []string
	localHostStartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		started = append(started, name)
		return &exec.Cmd{}, nil
	}

	localHostWaitManagedChildren = func(ctx context.Context, children []localHostChild, allowCleanExit string) error {
		return nil
	}

	err := runOwnedLocalHostWithLayout(context.Background(), eshulocal.Layout{
		WorkspaceID:     "workspace-id",
		WorkspaceRoot:   "/workspace/repo",
		OwnerRecordPath: "/workspace/owner.json",
		CacheDir:        "/workspace/cache",
		LogsDir:         "/workspace/logs",
		GraphDir:        "/workspace/graph",
	}, localHostModeWatch)
	if err != nil {
		t.Fatalf("runOwnedLocalHostWithLayout() error = %v, want nil", err)
	}

	wantChildren := []string{"eshu-ingester"}
	if !slices.Equal(started, wantChildren) {
		t.Fatalf("started children = %#v, want %#v", started, wantChildren)
	}
}

func TestWaitLocalHostChildrenReturnsReducerExitError(t *testing.T) {
	originalWaitChild := localHostWaitChildProcess
	t.Cleanup(func() {
		localHostWaitChildProcess = originalWaitChild
	})

	reducerCmd := &exec.Cmd{}
	ingesterCmd := &exec.Cmd{}
	reducerErr := errors.New("reducer exited")

	localHostWaitChildProcess = func(ctx context.Context, cmd *exec.Cmd) error {
		switch cmd {
		case reducerCmd:
			return reducerErr
		case ingesterCmd:
			<-ctx.Done()
			return nil
		default:
			t.Fatalf("unexpected child %p", cmd)
			return nil
		}
	}

	err := waitLocalHostChildren(context.Background(), []localHostChild{
		{name: "eshu-reducer", cmd: reducerCmd},
		{name: "eshu-ingester", cmd: ingesterCmd},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "eshu-reducer exited") {
		t.Fatalf("waitLocalHostChildren() error = %v, want reducer exit error", err)
	}
}

func TestWaitLocalHostChildrenAllowsMCPCleanExit(t *testing.T) {
	originalWaitChild := localHostWaitChildProcess
	t.Cleanup(func() {
		localHostWaitChildProcess = originalWaitChild
	})

	reducerCmd := &exec.Cmd{}
	ingesterCmd := &exec.Cmd{}
	mcpCmd := &exec.Cmd{}

	localHostWaitChildProcess = func(ctx context.Context, cmd *exec.Cmd) error {
		switch cmd {
		case mcpCmd:
			return nil
		case reducerCmd, ingesterCmd:
			<-ctx.Done()
			return nil
		default:
			t.Fatalf("unexpected child %p", cmd)
			return nil
		}
	}

	err := waitLocalHostChildren(context.Background(), []localHostChild{
		{name: "eshu-reducer", cmd: reducerCmd},
		{name: "eshu-ingester", cmd: ingesterCmd},
		{name: "eshu-mcp-server", cmd: mcpCmd},
	}, "eshu-mcp-server")
	if err != nil {
		t.Fatalf("waitLocalHostChildren() error = %v, want nil", err)
	}
}

func TestWaitLocalChildProcessPreservesUnexpectedExitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local graph support is Unix-only for this chunk")
	}

	cmd := exec.Command("/bin/sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}

	err := waitLocalChildProcess(context.Background(), cmd)
	if err == nil {
		t.Fatal("waitLocalChildProcess() error = nil, want exit error")
	}
	if !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("waitLocalChildProcess() error = %v, want exit status", err)
	}
}

func TestWaitLocalHostChildrenKeepingAllowedCleanExitsKeepsOwnerAlive(t *testing.T) {
	originalWaitChild := localHostWaitChildProcess
	originalLogger := slog.Default()
	t.Cleanup(func() {
		localHostWaitChildProcess = originalWaitChild
		slog.SetDefault(originalLogger)
	})

	logs := &lockedBuffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{})))

	reducerCmd := &exec.Cmd{}
	ingesterCmd := &exec.Cmd{}
	localHostWaitChildProcess = func(ctx context.Context, cmd *exec.Cmd) error {
		switch cmd {
		case ingesterCmd:
			return nil
		case reducerCmd:
			<-ctx.Done()
			return nil
		default:
			t.Fatalf("unexpected child %p", cmd)
			return nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- waitLocalHostChildrenKeepingAllowedCleanExits(ctx, []localHostChild{
			{name: "eshu-reducer", cmd: reducerCmd},
			{name: "eshu-ingester", cmd: ingesterCmd},
		}, map[string]struct{}{"eshu-ingester": {}})
	}()

	select {
	case err := <-done:
		t.Fatalf("waitLocalHostChildrenKeepingAllowedCleanExits() returned early with %v, want owner to stay alive", err)
	case <-time.After(50 * time.Millisecond):
	}
	if got := logs.String(); !strings.Contains(got, "local host child exited cleanly") || !strings.Contains(got, "child=eshu-ingester") {
		t.Fatalf("allowed clean-exit log = %q, want child lifecycle message", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("waitLocalHostChildrenKeepingAllowedCleanExits() after cancel error = %v, want nil", err)
	}
}

// lockedBuffer lets the supervision race test inspect slog output while child
// watcher goroutines may still be flushing a log record.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestWaitLocalHostChildrenKeepingAllowedCleanExitsRejectsDisallowedCleanExit(t *testing.T) {
	originalWaitChild := localHostWaitChildProcess
	t.Cleanup(func() {
		localHostWaitChildProcess = originalWaitChild
	})

	reducerCmd := &exec.Cmd{}
	ingesterCmd := &exec.Cmd{}
	localHostWaitChildProcess = func(ctx context.Context, cmd *exec.Cmd) error {
		switch cmd {
		case reducerCmd:
			return nil
		case ingesterCmd:
			<-ctx.Done()
			return nil
		default:
			t.Fatalf("unexpected child %p", cmd)
			return nil
		}
	}

	err := waitLocalHostChildrenKeepingAllowedCleanExits(context.Background(), []localHostChild{
		{name: "eshu-reducer", cmd: reducerCmd},
		{name: "eshu-ingester", cmd: ingesterCmd},
	}, map[string]struct{}{"eshu-ingester": {}})
	if err == nil || !strings.Contains(err.Error(), "eshu-reducer exited unexpectedly") {
		t.Fatalf("waitLocalHostChildrenKeepingAllowedCleanExits() error = %v, want reducer unexpected-exit error", err)
	}
}

func TestWaitLocalHostChildrenKeepingAllowedCleanExitsReturnsChildError(t *testing.T) {
	originalWaitChild := localHostWaitChildProcess
	t.Cleanup(func() {
		localHostWaitChildProcess = originalWaitChild
	})

	reducerCmd := &exec.Cmd{}
	ingesterCmd := &exec.Cmd{}
	reducerErr := errors.New("reducer exited")
	localHostWaitChildProcess = func(ctx context.Context, cmd *exec.Cmd) error {
		switch cmd {
		case reducerCmd:
			return reducerErr
		case ingesterCmd:
			<-ctx.Done()
			return nil
		default:
			t.Fatalf("unexpected child %p", cmd)
			return nil
		}
	}

	err := waitLocalHostChildrenKeepingAllowedCleanExits(context.Background(), []localHostChild{
		{name: "eshu-reducer", cmd: reducerCmd},
		{name: "eshu-ingester", cmd: ingesterCmd},
	}, map[string]struct{}{"eshu-ingester": {}})
	if err == nil || !strings.Contains(err.Error(), "eshu-reducer exited: reducer exited") {
		t.Fatalf("waitLocalHostChildrenKeepingAllowedCleanExits() error = %v, want reducer exit error", err)
	}
}
