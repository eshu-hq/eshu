package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
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
		delay:     20 * time.Millisecond,
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

	scipFiles, usedAny, err := snapshotter.collectSCIPLanguageGroupFiles(
		context.Background(),
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

type concurrentSCIPIndexer struct {
	mu        sync.Mutex
	available map[string]bool
	active    int
	max       int
	delay     time.Duration
}

func (i *concurrentSCIPIndexer) IsAvailable(language string) bool {
	return i.available[language]
}

func (i *concurrentSCIPIndexer) Run(ctx context.Context, projectPath string, language string, outputDir string) (string, error) {
	i.mu.Lock()
	i.active++
	if i.active > i.max {
		i.max = i.active
	}
	i.mu.Unlock()
	defer func() {
		i.mu.Lock()
		i.active--
		i.mu.Unlock()
	}()

	select {
	case <-time.After(i.delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}

	return filepath.Join(outputDir, language+".scip"), nil
}

func (i *concurrentSCIPIndexer) maxActive() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.max
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
