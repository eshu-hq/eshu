package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestRenderLocalHostProgressSnapshotIncludesKnownWorkTableAndQueue(t *testing.T) {
	t.Parallel()

	rendered := renderLocalHostProgressSnapshot(
		"/workspace/repo",
		localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
		statuspkg.Report{
			AsOf: time.Date(2026, time.April, 23, 21, 15, 0, 0, time.UTC),
			Health: statuspkg.HealthSummary{
				State: "progressing",
			},
			GenerationHistory: statuspkg.GenerationHistorySnapshot{
				Active:     1,
				Pending:    2,
				Completed:  3,
				Superseded: 20,
				Failed:     1,
			},
			StageSummaries: []statuspkg.StageSummary{
				{Stage: "projector", Pending: 3, Claimed: 1, Running: 2, Retrying: 1, Succeeded: 4, DeadLetter: 1},
				{Stage: "reducer", Pending: 1, Succeeded: 7},
			},
			Queue: statuspkg.QueueSnapshot{
				Pending:              3,
				InFlight:             2,
				Retrying:             1,
				DeadLetter:           0,
				Failed:               0,
				OldestOutstandingAge: 5*time.Minute + 2*time.Second,
			},
			LatestQueueFailure: &statuspkg.QueueFailureSnapshot{
				Stage:          "reducer",
				Domain:         "code_call_materialization",
				Status:         "retrying",
				FailureClass:   "graph_write_timeout",
				FailureMessage: "neo4j execute group timed out after 2s",
				FailureDetails: "phase=semantic label=Variable rows=500",
			},
		},
	)

	for _, want := range []string{
		"Local progress 2026-04-23T21:15:00Z",
		"Owner: running | profile=local_authoritative | backend=nornicdb | workspace=/workspace/repo",
		"Health: progressing",
		"Stage      State       Progress        Done   Active  Waiting  Failed  Unit",
		"Collector  attention   [######------]  4/7    0       2        1       generations",
		"Projector  attention   [####--------]  4/12   3       4        1       work items",
		"Reducer    waiting     [##########--]  7/8    0       1        0       work items",
		"Superseded generations: 20",
		"Queue: pending=3 in_flight=2 retrying=1 dead_letter=0 failed=0 oldest=5m2s",
		"Latest failure: stage=reducer domain=code_call_materialization status=retrying class=graph_write_timeout",
		"message=\"neo4j execute group timed out after 2s\" details=\"phase=semantic label=Variable rows=500\"",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered progress missing %q in %q", want, rendered)
		}
	}
}

func TestRenderLocalHostProgressSnapshotShowsIdleWhenNoKnownWork(t *testing.T) {
	t.Parallel()

	rendered := renderLocalHostProgressSnapshot(
		"/workspace/repo",
		localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
		statuspkg.Report{
			AsOf: time.Date(2026, time.April, 23, 21, 15, 0, 0, time.UTC),
			Health: statuspkg.HealthSummary{
				State: "healthy",
			},
		},
	)

	for _, want := range []string{
		"Collector  idle",
		"Projector  idle",
		"Reducer    idle",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered progress missing idle row %q in %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "0/0") {
		t.Fatalf("rendered progress = %q, want idle state instead of 0/0 denominator", rendered)
	}
}

func TestLocalHostProgressFingerprintIgnoresAsOfAndBucketsAge(t *testing.T) {
	t.Parallel()

	runtimeConfig := localHostRuntimeConfig{
		Profile:      query.ProfileLocalAuthoritative,
		GraphBackend: query.GraphBackendNornicDB,
	}
	base := statuspkg.Report{
		AsOf: time.Date(2026, time.April, 23, 21, 15, 0, 0, time.UTC),
		Health: statuspkg.HealthSummary{
			State: "progressing",
		},
		ScopeTotals: map[string]int{
			"pending": 1,
		},
		GenerationTotals: map[string]int{
			"pending": 1,
		},
		StageSummaries: []statuspkg.StageSummary{
			{Stage: "projector", Claimed: 1},
		},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{Domain: "source_local", Outstanding: 1, OldestAge: 35 * time.Second},
		},
		FlowSummaries: []statuspkg.FlowSummary{
			{Lane: "projector", Progress: "stage claimed=1", Backlog: "queue outstanding=1 oldest=35s"},
		},
		Queue: statuspkg.QueueSnapshot{
			Pending:              0,
			InFlight:             1,
			Retrying:             0,
			DeadLetter:           0,
			Failed:               0,
			OldestOutstandingAge: 35 * time.Second,
		},
	}

	sameBucket := base
	sameBucket.AsOf = sameBucket.AsOf.Add(3 * time.Second)
	sameBucket.FlowSummaries = []statuspkg.FlowSummary{
		{Lane: "projector", Progress: "stage claimed=1", Backlog: "queue outstanding=1 oldest=55s"},
	}
	sameBucket.DomainBacklogs = []statuspkg.DomainBacklog{
		{Domain: "source_local", Outstanding: 1, OldestAge: 55 * time.Second},
	}
	sameBucket.Queue.OldestOutstandingAge = 55 * time.Second

	if got, want := localHostProgressFingerprint("/workspace/repo", runtimeConfig, sameBucket), localHostProgressFingerprint("/workspace/repo", runtimeConfig, base); got != want {
		t.Fatalf("progress fingerprint changed within the same age bucket: got %q want %q", got, want)
	}

	nextBucket := base
	nextBucket.Queue.OldestOutstandingAge = 61 * time.Second
	if got, want := localHostProgressFingerprint("/workspace/repo", runtimeConfig, nextBucket), localHostProgressFingerprint("/workspace/repo", runtimeConfig, base); got == want {
		t.Fatal("progress fingerprint stayed the same across a new age bucket")
	}

	withFailure := base
	withFailure.LatestQueueFailure = &statuspkg.QueueFailureSnapshot{
		Stage:          "reducer",
		Domain:         "code_call_materialization",
		Status:         "retrying",
		FailureClass:   "graph_write_timeout",
		FailureMessage: "neo4j execute group timed out after 2s",
		FailureDetails: "phase=semantic label=Variable rows=500",
	}
	if got, want := localHostProgressFingerprint("/workspace/repo", runtimeConfig, withFailure), localHostProgressFingerprint("/workspace/repo", runtimeConfig, base); got == want {
		t.Fatal("progress fingerprint stayed the same after latest failure details changed")
	}
}

func TestLocalHostProgressEnabledHonorsQuietMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "unset defaults on", want: true},
		{name: "auto", env: "auto", want: true},
		{name: "plain", env: "plain", want: true},
		{name: "quiet", env: "quiet", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := localHostProgressEnabled(func(key string) string {
				if key == localHostProgressModeEnv {
					return tt.env
				}
				return ""
			})
			if got != tt.want {
				t.Fatalf("localHostProgressEnabled() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRunOwnedLocalHostWithLayoutWatchStartsAndStopsProgressReporter(t *testing.T) {
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
	originalStartProgressReporter := localHostStartProgressReporter
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
		localHostStartProgressReporter = originalStartProgressReporter
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
	localHostStartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		return &exec.Cmd{}, nil
	}
	localHostContentSearchIndexExpectedProjectors = func(workspaceRoot string) (int, error) {
		return 1, nil
	}
	localHostStartIaCReachabilityFinalizer = func(ctx context.Context, dsn string, expectedProjectors int) (func() error, error) {
		return func() error { return nil }, nil
	}

	progressStarts := 0
	progressStops := 0
	localHostStartProgressReporter = func(
		ctx context.Context,
		workspaceRoot string,
		dsn string,
		runtimeConfig localHostRuntimeConfig,
	) (localHostProgressStop, error) {
		progressStarts++
		if workspaceRoot != "/workspace/repo" {
			t.Fatalf("progress reporter workspace root = %q, want /workspace/repo", workspaceRoot)
		}
		if runtimeConfig.Profile != query.ProfileLocalAuthoritative {
			t.Fatalf("progress reporter profile = %q, want %q", runtimeConfig.Profile, query.ProfileLocalAuthoritative)
		}
		return func() error {
			progressStops++
			return nil
		}, nil
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

	if got, want := progressStarts, 1; got != want {
		t.Fatalf("progress reporter starts = %d, want %d", got, want)
	}
	if got, want := progressStops, 1; got != want {
		t.Fatalf("progress reporter stops = %d, want %d", got, want)
	}
}
