package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/collector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// runnerFunc adapts a plain function into the app.Runner interface for tests.
type runnerFunc func(context.Context) error

func (f runnerFunc) Run(ctx context.Context) error { return f(ctx) }

var _ app.Runner = runnerFunc(nil)

func TestNornicDBTuningDocCanonicalDefaultsMatchCode(t *testing.T) {
	t.Parallel()

	doc := readNornicDBTuningDoc(t)
	cases := map[string]string{
		nornicDBPhaseGroupStatementsEnv:            fmt.Sprint(defaultNornicDBPhaseGroupStatements),
		nornicDBFilePhaseGroupStatementsEnv:        fmt.Sprint(defaultNornicDBFilePhaseStatements),
		nornicDBFileBatchSizeEnv:                   fmt.Sprint(defaultNornicDBFileBatchSize),
		nornicDBEntityPhaseStatementsEnv:           fmt.Sprint(defaultNornicDBEntityPhaseStatements),
		nornicDBEntityBatchSizeEnv:                 fmt.Sprint(defaultNornicDBEntityBatchSize),
		nornicDBEntityLabelBatchSizesEnv:           formatLabelSizes(defaultNornicDBEntityLabelBatchSizes(0)),
		nornicDBEntityLabelPhaseGroupStatementsEnv: formatLabelSizes(defaultNornicDBEntityLabelPhaseGroupStatements(0)),
		nornicDBCanonicalGroupedWritesEnv:          "unset / false",
		nornicDBBatchedEntityContainmentEnv:        "unset / true",
		canonicalWriteTimeoutEnv:                   fmt.Sprintf("%s on NornicDB", defaultNornicDBCanonicalWriteTimeout),
	}
	for envName, wantDefault := range cases {
		envName, wantDefault := envName, wantDefault
		t.Run(envName, func(t *testing.T) {
			t.Parallel()

			gotDefault, ok := markdownTableDefault(doc, envName)
			if !ok {
				t.Fatalf("nornicdb tuning doc missing %s", envName)
			}
			if gotDefault != wantDefault {
				t.Fatalf("doc default for %s = %q, want %q", envName, gotDefault, wantDefault)
			}
		})
	}
}

func TestProjectorWorkerCountDefaultsToCPUForNornicDBLocalAuthoritative(t *testing.T) {
	t.Parallel()

	got := projectorWorkerCount(func(key string) string {
		switch key {
		case "ESHU_QUERY_PROFILE":
			return "local_authoritative"
		case "ESHU_GRAPH_BACKEND":
			return "nornicdb"
		default:
			return ""
		}
	})
	if got != runtime.NumCPU() {
		t.Fatalf("projectorWorkerCount() = %d, want runtime.NumCPU()", got)
	}
}

func TestProjectorWorkerCountKeepsExplicitOverride(t *testing.T) {
	t.Parallel()

	got := projectorWorkerCount(func(key string) string {
		switch key {
		case "ESHU_PROJECTOR_WORKERS":
			return "3"
		case "ESHU_QUERY_PROFILE":
			return "local_authoritative"
		case "ESHU_GRAPH_BACKEND":
			return "nornicdb"
		default:
			return ""
		}
	})
	if got != 3 {
		t.Fatalf("projectorWorkerCount() = %d, want explicit override", got)
	}
}

func TestBuildIngesterServiceProducesCompositeRunner(t *testing.T) {
	t.Parallel()

	runner, err := buildIngesterService(
		postgres.SQLDB{},
		&noopCanonicalWriter{},
		func(string) string { return "" },
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil, // tracer
		nil, // instruments
		nil, // logger
	)
	if err != nil {
		t.Fatalf("buildIngesterService() error = %v, want nil", err)
	}
	if len(runner.runners) != 2 {
		t.Fatalf("buildIngesterService() runner count = %d, want 2", len(runner.runners))
	}
}

func readNornicDBTuningDoc(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	docPath := filepath.Join(filepath.Dir(filename), "..", "..", "..", "docs", "docs", "reference", "nornicdb-tuning.md")
	contents, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read nornicdb tuning doc: %v", err)
	}
	return string(contents)
}

func markdownTableDefault(markdown string, envName string) (string, bool) {
	prefix := "| `" + envName + "` |"
	for _, line := range strings.Split(markdown, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			return "", false
		}
		return normalizeMarkdownDefault(cells[2]), true
	}
	return "", false
}

func normalizeMarkdownDefault(defaultCell string) string {
	return strings.ReplaceAll(strings.TrimSpace(defaultCell), "`", "")
}

func formatLabelSizes(labelSizes map[string]int) string {
	var builder strings.Builder
	for i, label := range orderedEntityBatchLabels(labelSizes) {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(label)
		builder.WriteByte('=')
		fmt.Fprint(&builder, labelSizes[label])
	}
	return builder.String()
}

func TestBuildIngesterCollectorServiceUsesNativeSnapshotter(t *testing.T) {
	t.Parallel()

	service, err := buildIngesterCollectorService(
		postgres.SQLDB{},
		func(string) string { return "" },
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil, // tracer
		nil, // instruments
		nil, // logger
	)
	if err != nil {
		t.Fatalf("buildIngesterCollectorService() error = %v, want nil", err)
	}

	source, ok := service.Source.(*collector.GitSource)
	if !ok {
		t.Fatalf("buildIngesterCollectorService() source type = %T, want *collector.GitSource", service.Source)
	}
	if _, ok := source.Selector.(collector.NativeRepositorySelector); !ok {
		t.Fatalf("buildIngesterCollectorService() selector type = %T, want collector.NativeRepositorySelector", source.Selector)
	}
	if _, ok := source.Snapshotter.(collector.NativeRepositorySnapshotter); !ok {
		t.Fatalf("buildIngesterCollectorService() snapshotter type = %T, want collector.NativeRepositorySnapshotter", source.Snapshotter)
	}
}

func TestBuildIngesterCollectorServiceWiresDiscoveryPathGlobOverlay(t *testing.T) {
	t.Parallel()

	service, err := buildIngesterCollectorService(
		postgres.SQLDB{},
		func(key string) string {
			if key == "ESHU_DISCOVERY_IGNORED_PATH_GLOBS" {
				return "generated/**=generated-template"
			}
			return ""
		},
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildIngesterCollectorService() error = %v, want nil", err)
	}

	source := service.Source.(*collector.GitSource)
	snapshotter := source.Snapshotter.(collector.NativeRepositorySnapshotter)
	if got, want := len(snapshotter.DiscoveryOptions.IgnoredPathGlobs), 1; got != want {
		t.Fatalf("IgnoredPathGlobs length = %d, want %d", got, want)
	}
	if got, want := snapshotter.DiscoveryOptions.IgnoredPathGlobs[0].Pattern, "generated/**"; got != want {
		t.Fatalf("IgnoredPathGlobs[0].Pattern = %q, want %q", got, want)
	}
}

func TestBuildIngesterCollectorServiceDefersRelationshipBackfillToBatchDrain(t *testing.T) {
	t.Parallel()

	service, err := buildIngesterCollectorService(
		postgres.SQLDB{},
		func(string) string { return "" },
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil, // tracer
		nil, // instruments
		nil, // logger
	)
	if err != nil {
		t.Fatalf("buildIngesterCollectorService() error = %v, want nil", err)
	}

	committer, ok := service.Committer.(postgres.IngestionStore)
	if !ok {
		t.Fatalf("Committer type = %T, want postgres.IngestionStore", service.Committer)
	}
	if !committer.SkipRelationshipBackfill {
		t.Fatal("Committer.SkipRelationshipBackfill = false, want true for deferred batch-drain backfill")
	}
	if service.AfterBatchDrained == nil {
		t.Fatal("AfterBatchDrained = nil, want deferred relationship maintenance hook")
	}
}

func TestBuildIngesterProjectorRuntimeWiresPhasePublisherAndRepairQueue(t *testing.T) {
	t.Parallel()

	runtime, err := buildIngesterProjectorRuntime(
		postgres.SQLDB{},
		&noopCanonicalWriter{},
		nil,
		nil,
		func(string) string { return "" },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildIngesterProjectorRuntime() error = %v, want nil", err)
	}

	if runtime.PhasePublisher == nil {
		t.Fatal("buildIngesterProjectorRuntime() PhasePublisher = nil, want non-nil")
	}
	if runtime.RepairQueue == nil {
		t.Fatal("buildIngesterProjectorRuntime() RepairQueue = nil, want non-nil")
	}
}

func TestCompositeRunnerCancelsOnFirstError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("runner failed")
	blocked := make(chan struct{})

	runner := compositeRunner{
		runners: []app.Runner{
			runnerFunc(func(ctx context.Context) error {
				return expectedErr
			}),
			runnerFunc(func(ctx context.Context) error {
				<-ctx.Done()
				close(blocked)
				return nil
			}),
		},
	}

	err := runner.Run(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("compositeRunner.Run() error = %v, want %v", err, expectedErr)
	}

	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("compositeRunner.Run() did not cancel remaining runners")
	}
}

func TestLargeGenThresholdDefault(t *testing.T) {
	t.Parallel()

	got := largeGenThreshold(func(string) string { return "" })
	if got != 10000 {
		t.Fatalf("largeGenThreshold() = %d, want 10000", got)
	}
}

func TestLargeGenThresholdFromEnv(t *testing.T) {
	t.Parallel()

	got := largeGenThreshold(func(k string) string {
		if k == "ESHU_LARGE_GEN_THRESHOLD" {
			return "5000"
		}
		return ""
	})
	if got != 5000 {
		t.Fatalf("largeGenThreshold() = %d, want 5000", got)
	}
}

func TestLargeGenMaxConcurrentDefault(t *testing.T) {
	t.Parallel()

	got := largeGenMaxConcurrent(func(string) string { return "" })
	if got != 2 {
		t.Fatalf("largeGenMaxConcurrent() = %d, want 2", got)
	}
}

func TestLargeGenMaxConcurrentLocalAuthoritativeDefault(t *testing.T) {
	t.Parallel()

	got := largeGenMaxConcurrent(func(k string) string {
		if k == "ESHU_QUERY_PROFILE" {
			return "local_authoritative"
		}
		return ""
	})
	if got != 4 {
		t.Fatalf("largeGenMaxConcurrent() = %d, want 4", got)
	}
}

func TestLargeGenMaxConcurrentFromEnv(t *testing.T) {
	t.Parallel()

	got := largeGenMaxConcurrent(func(k string) string {
		if k == "ESHU_LARGE_GEN_MAX_CONCURRENT" {
			return "4"
		}
		return ""
	})
	if got != 4 {
		t.Fatalf("largeGenMaxConcurrent() = %d, want 4", got)
	}
}

func TestCompositeRunnerExitsOnContextCancel(t *testing.T) {
	t.Parallel()

	runner := compositeRunner{
		runners: []app.Runner{
			runnerFunc(func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			}),
			runnerFunc(func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			}),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("compositeRunner.Run() error = %v, want nil", err)
	}
}

func TestOpenIngesterCanonicalWriterAcceptsNornicDBOnSharedBoltPath(t *testing.T) {
	t.Parallel()

	_, closer, err := openIngesterCanonicalWriter(context.Background(), func(key string) string {
		switch key {
		case "ESHU_GRAPH_BACKEND":
			return "nornicdb"
		default:
			return ""
		}
	}, nil, nil)
	if closer != nil {
		_ = closer.Close()
	}
	if err == nil {
		t.Fatal("openIngesterCanonicalWriter() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ESHU_NEO4J_URI") && !strings.Contains(err.Error(), "NEO4J_URI") {
		t.Fatalf("openIngesterCanonicalWriter() error = %q, want shared bolt config context", err)
	}
}

func TestCanonicalExecutorForGraphBackendKeepsNeo4jGrouped(t *testing.T) {
	t.Parallel()

	executor := canonicalExecutorForGraphBackend(
		&groupCapableIngesterExecutor{},
		runtimecfg.GraphBackendNeo4j,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)
	if _, ok := executor.(sourcecypher.GroupExecutor); !ok {
		t.Fatal("Neo4j canonical executor does not implement GroupExecutor")
	}
}

func TestCanonicalExecutorForGraphBackendUsesNornicDBPhaseGroupsByDefault(t *testing.T) {
	t.Parallel()

	inner := &groupCapableIngesterExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)
	if _, ok := executor.(sourcecypher.GroupExecutor); ok {
		t.Fatal("NornicDB canonical executor implements GroupExecutor, want non-atomic phase-group surface")
	}
	pge, ok := executor.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("NornicDB canonical executor does not implement PhaseGroupExecutor by default")
	}

	err := executor.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if inner.executeCalls != 1 {
		t.Fatalf("inner Execute calls = %d, want 1", inner.executeCalls)
	}
	if err := pge.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{{Cypher: "RETURN 2"}}); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if inner.groupCalls != 1 {
		t.Fatalf("inner ExecuteGroup calls = %d, want 1", inner.groupCalls)
	}
}
