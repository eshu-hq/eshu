package main

import (
	"context"
	"os/exec"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestRunOwnedLocalHostWithLayoutAuthoritativeStartsManagedGraph(t *testing.T) {
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
		if runtimeConfig.GraphBackend != query.GraphBackendNornicDB {
			t.Fatalf("runtimeConfig.GraphBackend = %q, want %q", runtimeConfig.GraphBackend, query.GraphBackendNornicDB)
		}
		return &managedLocalGraph{
			Backend:    query.GraphBackendNornicDB,
			Version:    "1.0.42",
			BinaryPath: "/tmp/nornicdb",
			Address:    "127.0.0.1",
			BoltPort:   17687,
			HTTPPort:   17474,
			DataDir:    "/workspace/graph/nornicdb",
			LogPath:    "/workspace/logs/graph-nornicdb.log",
			Username:   "admin",
			Password:   "workspace-secret",
			PID:        88,
			Cmd:        &exec.Cmd{},
		}, nil
	}
	localHostHostname = func() (string, error) {
		return "local-test", nil
	}
	localHostApplyBootstrap = func(ctx context.Context, dsn string) error {
		return nil
	}
	graphBootstrapped := false
	localHostApplyGraphBootstrap = func(ctx context.Context, runtimeConfig localHostRuntimeConfig, graph *managedLocalGraph) error {
		if runtimeConfig.GraphBackend != query.GraphBackendNornicDB {
			t.Fatalf("graph bootstrap backend = %q, want %q", runtimeConfig.GraphBackend, query.GraphBackendNornicDB)
		}
		if graph == nil || graph.BoltPort != 17687 {
			t.Fatalf("graph bootstrap managed graph = %#v, want bolt port 17687", graph)
		}
		graphBootstrapped = true
		return nil
	}
	localHostContentSearchIndexExpectedProjectors = func(workspaceRoot string) (int, error) {
		if workspaceRoot != "/workspace/repo" {
			t.Fatalf("workspaceRoot = %q, want /workspace/repo", workspaceRoot)
		}
		return 2, nil
	}
	finalizerStarted := false
	localHostStartIaCReachabilityFinalizer = func(ctx context.Context, dsn string, expectedProjectors int) (func() error, error) {
		if expectedProjectors != 2 {
			t.Fatalf("expectedProjectors = %d, want 2", expectedProjectors)
		}
		finalizerStarted = true
		return func() error { return nil }, nil
	}
	var written eshulocal.OwnerRecord
	localHostWriteOwnerRecord = func(path string, record eshulocal.OwnerRecord) error {
		if !graphBootstrapped {
			t.Fatal("owner record written before local graph schema bootstrap")
		}
		written = record
		return nil
	}
	var started []string
	localHostStartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		if !graphBootstrapped {
			t.Fatalf("%s started before local graph schema bootstrap", name)
		}
		started = append(started, name)
		if envValue(env, "ESHU_NEO4J_URI") != "bolt://127.0.0.1:17687" {
			t.Fatalf("ESHU_NEO4J_URI = %q, want %q", envValue(env, "ESHU_NEO4J_URI"), "bolt://127.0.0.1:17687")
		}
		if name == "eshu-reducer" && envValue(env, "ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS") != "2" {
			t.Fatalf("ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS = %q, want 2", envValue(env, "ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS"))
		}
		return &exec.Cmd{}, nil
	}
	localHostWaitManagedChildren = func(ctx context.Context, children []localHostChild, allowCleanExit string) error {
		return nil
	}
	localHostWaitOwnerChildren = func(ctx context.Context, children []localHostChild, allowedCleanExits map[string]struct{}) error {
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
	if written.GraphPID != 88 {
		t.Fatalf("written.GraphPID = %d, want %d", written.GraphPID, 88)
	}
	if written.GraphBackend != string(query.GraphBackendNornicDB) {
		t.Fatalf("written.GraphBackend = %q, want %q", written.GraphBackend, query.GraphBackendNornicDB)
	}
	if written.GraphBoltPort != 17687 {
		t.Fatalf("written.GraphBoltPort = %d, want %d", written.GraphBoltPort, 17687)
	}
	if written.GraphHTTPPort != 17474 {
		t.Fatalf("written.GraphHTTPPort = %d, want %d", written.GraphHTTPPort, 17474)
	}
	if written.GraphVersion != "1.0.42" {
		t.Fatalf("written.GraphVersion = %q, want %q", written.GraphVersion, "1.0.42")
	}
	if written.GraphUsername != "admin" {
		t.Fatalf("written.GraphUsername = %q, want %q", written.GraphUsername, "admin")
	}
	if written.GraphPassword != "workspace-secret" {
		t.Fatalf("written.GraphPassword = %q, want %q", written.GraphPassword, "workspace-secret")
	}
	if got, want := started, []string{"eshu-reducer", "eshu-ingester"}; !slices.Equal(got, want) {
		t.Fatalf("started children = %#v, want %#v", got, want)
	}
	if !finalizerStarted {
		t.Fatal("IaC reachability finalizer was not started for local authoritative owner")
	}
}
