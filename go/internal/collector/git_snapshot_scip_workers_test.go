// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestLoadSnapshotSCIPConfigParsesWorkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "empty", raw: "", want: 4},
		{name: "single_worker_override", raw: "1", want: 1},
		{name: "positive", raw: "3", want: 3},
		{name: "zero", raw: "0", want: 4},
		{name: "invalid", raw: "many", want: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := LoadSnapshotSCIPConfig(func(key string) string {
				if key == "SCIP_WORKERS" {
					return test.raw
				}
				return ""
			})

			if got := config.Workers; got != test.want {
				t.Fatalf("Workers = %d, want %d", got, test.want)
			}
		})
	}
}

func TestLoadSnapshotSCIPConfigDefaultsToConcurrentWorkers(t *testing.T) {
	t.Parallel()

	config := LoadSnapshotSCIPConfig(func(string) string {
		return ""
	})

	if got, want := config.Workers, 4; got != want {
		t.Fatalf("Workers = %d, want concurrent default %d", got, want)
	}
}

func TestSCIPLanguageSubtreesRunWithBoundedWorkers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiRoot := filepath.Join(repoRoot, "services", "api")
	jobsRoot := filepath.Join(repoRoot, "services", "jobs")
	apiPath := filepath.Join(apiRoot, "app.py")
	jobsPath := filepath.Join(jobsRoot, "worker.py")
	writeCollectorTestFile(t, filepath.Join(apiRoot, "pyproject.toml"), "[project]\nname = \"api\"\n")
	writeCollectorTestFile(t, filepath.Join(jobsRoot, "pyproject.toml"), "[project]\nname = \"jobs\"\n")
	writeCollectorTestFile(t, apiPath, "def main():\n    return helper()\n")
	writeCollectorTestFile(t, jobsPath, "def run():\n    return task()\n")

	indexer := &concurrentSCIPIndexer{
		available: map[string]bool{"python": true},
		barrier:   make(chan struct{}),
		waitFor:   2,
	}
	resultParser := rootSCIPParser{resultsByRoot: map[string]parser.SCIPParseResult{
		apiRoot: {
			Files: map[string]map[string]any{
				apiPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python api/main()."}}},
			},
		},
		jobsRoot: {
			Files: map[string]map[string]any{
				jobsPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python jobs/run()."}}},
			},
		},
	}}
	snapshotter := NativeRepositorySnapshotter{SCIP: SnapshotSCIPConfig{Workers: 2}}
	groups := parser.DetectSCIPProjectLanguageGroups([]string{apiPath, jobsPath}, []string{"python"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scipFiles, usedAny, err := snapshotter.collectSCIPLanguageGroupFiles(
		ctx,
		repoRoot,
		groups,
		indexer,
		resultParser,
	)
	if err != nil {
		t.Fatalf("collectSCIPLanguageGroupFiles() error = %v, want nil", err)
	}
	if !usedAny {
		t.Fatal("usedAny = false, want true")
	}
	if got, want := len(scipFiles), 2; got != want {
		t.Fatalf("len(scipFiles) = %d, want %d", got, want)
	}
	if got := indexer.maxActive(); got < 2 {
		t.Fatalf("max concurrent SCIP runs = %d, want at least 2", got)
	}
}

func TestSCIPWorkersCapConcurrentSnapshots(t *testing.T) {
	config := LoadSnapshotSCIPConfig(func(key string) string {
		if key == "SCIP_WORKERS" {
			return "2"
		}
		return ""
	})

	first := scipWorkerTestRepo(t, "first")
	second := scipWorkerTestRepo(t, "second")
	indexer := &concurrentSCIPIndexer{
		available: map[string]bool{"python": true},
		delay:     50 * time.Millisecond,
	}
	resultParser := rootSCIPParser{resultsByRoot: mergeSCIPWorkerResults(first.results, second.results)}

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, repo := range []scipWorkerRepo{first, second} {
		repo := repo
		go func() {
			<-start
			snapshotter := NativeRepositorySnapshotter{SCIP: config}
			scipFiles, usedAny, err := snapshotter.collectSCIPLanguageGroupFiles(
				context.Background(),
				repo.root,
				parser.DetectSCIPProjectLanguageGroups(repo.files, []string{"python"}),
				indexer,
				resultParser,
			)
			if err != nil {
				errs <- err
				return
			}
			if !usedAny || len(scipFiles) != len(repo.files) {
				errs <- fmt.Errorf("SCIP files = %d used=%v, want %d true", len(scipFiles), usedAny, len(repo.files))
				return
			}
			errs <- nil
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got, want := indexer.maxActive(), 2; got > want {
		t.Fatalf("max concurrent SCIP runs = %d, want at most %d", got, want)
	}
}

func TestSCIPWorkersRecordLimiterWaitDuration(t *testing.T) {
	config := LoadSnapshotSCIPConfig(func(key string) string {
		if key == "SCIP_WORKERS" {
			return "1"
		}
		return ""
	})

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-scip-wait-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	first := scipWorkerTestRepo(t, "first")
	second := scipWorkerTestRepo(t, "second")
	indexer := &concurrentSCIPIndexer{
		available: map[string]bool{"python": true},
		delay:     20 * time.Millisecond,
	}
	resultParser := rootSCIPParser{resultsByRoot: mergeSCIPWorkerResults(first.results, second.results)}

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, repo := range []scipWorkerRepo{first, second} {
		repo := repo
		go func() {
			<-start
			snapshotter := NativeRepositorySnapshotter{SCIP: config, Instruments: instruments}
			scipFiles, usedAny, err := snapshotter.collectSCIPLanguageGroupFiles(
				context.Background(),
				repo.root,
				parser.DetectSCIPProjectLanguageGroups(repo.files, []string{"python"}),
				indexer,
				resultParser,
			)
			if err != nil {
				errs <- err
				return
			}
			if !usedAny || len(scipFiles) != len(repo.files) {
				errs <- fmt.Errorf("SCIP files = %d used=%v, want %d true", len(scipFiles), usedAny, len(repo.files))
				return
			}
			errs <- nil
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	attrs := map[string]string{"language": "python"}
	if got, want := scipHistogramCount(t, rm, "eshu_dp_scip_process_wait_seconds", attrs), uint64(4); got != want {
		t.Fatalf("eshu_dp_scip_process_wait_seconds{language=python} count = %d, want %d", got, want)
	}
	if got := scipHistogramSum(t, rm, "eshu_dp_scip_process_wait_seconds", attrs); got <= 0 {
		t.Fatalf("eshu_dp_scip_process_wait_seconds{language=python} sum = %f, want > 0", got)
	}
}

func TestSCIPLanguageGroupFilesConcurrentPreservesSubtreeMergeOrder(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	childRoot := filepath.Join(repoRoot, "services", "api")
	filePath := filepath.Join(childRoot, "app.py")
	writeCollectorTestFile(t, filePath, "def main():\n    return helper()\n")

	indexer := delayedSCIPIndexer{
		available: map[string]bool{"python": true},
		delays: map[string]time.Duration{
			repoRoot:  40 * time.Millisecond,
			childRoot: time.Millisecond,
		},
	}
	resultParser := rootSCIPParser{resultsByRoot: map[string]parser.SCIPParseResult{
		repoRoot: {
			Files: map[string]map[string]any{
				filePath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python parent/main()."}}},
			},
		},
		childRoot: {
			Files: map[string]map[string]any{
				filePath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python child/main()."}}},
			},
		},
	}}
	snapshotter := NativeRepositorySnapshotter{SCIP: SnapshotSCIPConfig{Workers: 2}}

	scipFiles, usedAny, err := snapshotter.collectSCIPLanguageGroupFilesConcurrent(
		context.Background(),
		[]scipLanguageSubtree{
			{Language: "python", Root: repoRoot},
			{Language: "python", Root: childRoot},
		},
		indexer,
		resultParser,
		2,
	)
	if err != nil {
		t.Fatalf("collectSCIPLanguageGroupFilesConcurrent() error = %v, want nil", err)
	}
	if !usedAny {
		t.Fatal("usedAny = false, want true")
	}
	calls, ok := scipFiles[filePath]["function_calls_scip"].([]map[string]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("function_calls_scip = %#v, want one deterministic SCIP call", scipFiles[filePath]["function_calls_scip"])
	}
	if got, want := calls[0]["callee_symbol"], "scip-python python child/main()."; got != want {
		t.Fatalf("callee_symbol = %#v, want %#v", got, want)
	}
}

type scipWorkerRepo struct {
	root    string
	files   []string
	results map[string]parser.SCIPParseResult
}

func scipWorkerTestRepo(t *testing.T, name string) scipWorkerRepo {
	t.Helper()

	repoRoot := filepath.Join(t.TempDir(), name)
	apiRoot := filepath.Join(repoRoot, "services", "api")
	jobsRoot := filepath.Join(repoRoot, "services", "jobs")
	apiPath := filepath.Join(apiRoot, "app.py")
	jobsPath := filepath.Join(jobsRoot, "worker.py")
	writeCollectorTestFile(t, filepath.Join(apiRoot, "pyproject.toml"), "[project]\nname = \"api\"\n")
	writeCollectorTestFile(t, filepath.Join(jobsRoot, "pyproject.toml"), "[project]\nname = \"jobs\"\n")
	writeCollectorTestFile(t, apiPath, "def main():\n    return helper()\n")
	writeCollectorTestFile(t, jobsPath, "def run():\n    return task()\n")

	return scipWorkerRepo{
		root:  repoRoot,
		files: []string{apiPath, jobsPath},
		results: map[string]parser.SCIPParseResult{
			apiRoot: {
				Files: map[string]map[string]any{
					apiPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python api/main()."}}},
				},
			},
			jobsRoot: {
				Files: map[string]map[string]any{
					jobsPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python jobs/run()."}}},
				},
			},
		},
	}
}

func mergeSCIPWorkerResults(resultSets ...map[string]parser.SCIPParseResult) map[string]parser.SCIPParseResult {
	merged := make(map[string]parser.SCIPParseResult)
	for _, results := range resultSets {
		for root, result := range results {
			merged[root] = result
		}
	}
	return merged
}

func scipHistogramSum(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
) float64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf(
					"metric %s data = %T, want metricdata.Histogram[float64]",
					metricName,
					metricRecord.Data,
				)
			}
			for _, dp := range histogram.DataPoints {
				if collectorHasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Sum
				}
			}
		}
	}
	return 0
}

func BenchmarkSCIPLanguageSubtreeWorkers(b *testing.B) {
	for _, workers := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			repoRoot := b.TempDir()
			var files []string
			results := make(map[string]parser.SCIPParseResult)
			for service := 0; service < 4; service++ {
				root := filepath.Join(repoRoot, "services", fmt.Sprintf("svc-%02d", service))
				path := filepath.Join(root, "app.py")
				writeSCIPWorkerBenchmarkFile(b, filepath.Join(root, "pyproject.toml"), "[project]\nname = \"svc\"\n")
				writeSCIPWorkerBenchmarkFile(b, path, "def main():\n    return 1\n")
				files = append(files, path)
				results[root] = parser.SCIPParseResult{
					Files: map[string]map[string]any{
						path: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python main()."}}},
					},
				}
			}
			groups := parser.DetectSCIPProjectLanguageGroups(files, []string{"python"})
			snapshotter := NativeRepositorySnapshotter{SCIP: SnapshotSCIPConfig{Workers: workers}}
			indexer := &concurrentSCIPIndexer{
				available: map[string]bool{"python": true},
				delay:     5 * time.Millisecond,
			}
			resultParser := rootSCIPParser{resultsByRoot: results}
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				scipFiles, usedAny, err := snapshotter.collectSCIPLanguageGroupFiles(
					context.Background(),
					repoRoot,
					groups,
					indexer,
					resultParser,
				)
				if err != nil {
					b.Fatalf("collectSCIPLanguageGroupFiles() error = %v, want nil", err)
				}
				if !usedAny || len(scipFiles) != len(files) {
					b.Fatalf("SCIP files = %d used=%v, want %d true", len(scipFiles), usedAny, len(files))
				}
			}
		})
	}
}

type delayedSCIPIndexer struct {
	available map[string]bool
	delays    map[string]time.Duration
}

func (i delayedSCIPIndexer) IsAvailable(language string) bool {
	return i.available[language]
}

func (i delayedSCIPIndexer) Run(ctx context.Context, projectPath string, language string, outputDir string) (string, error) {
	delay := i.delays[projectPath]
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return filepath.Join(outputDir, language+".scip"), nil
}

type rootSCIPParser struct {
	resultsByRoot map[string]parser.SCIPParseResult
}

func (p rootSCIPParser) Parse(_ string, projectRoot string) (parser.SCIPParseResult, error) {
	return p.resultsByRoot[projectRoot], nil
}

func writeSCIPWorkerBenchmarkFile(b *testing.B, path string, body string) {
	b.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		b.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}
