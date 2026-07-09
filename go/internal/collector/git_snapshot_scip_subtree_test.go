// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSCIPSnapshotRunsEachSupportedLanguageSubtree(t *testing.T) {
	repoRoot := t.TempDir()
	pythonPath := filepath.Join(repoRoot, "services", "api", "app.py")
	goPath := filepath.Join(repoRoot, "services", "worker", "main.go")
	writeCollectorTestFile(t, pythonPath, "def main():\n    return helper()\n")
	writeCollectorTestFile(t, goPath, "package main\n\nfunc main() {}\n")

	indexer := &languagePathSCIPIndexer{available: map[string]bool{"python": true, "go": true}}
	config := LoadSnapshotSCIPConfig(scipEnabledTestGetenv(nil))
	config.Workers = 1
	config.Indexer = indexer
	config.Parser = languagePathSCIPParser{
		results: map[string]parser.SCIPParseResult{
			"python": {
				Files: map[string]map[string]any{
					pythonPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python app/main()."}}},
				},
			},
			"go": {
				Files: map[string]map[string]any{
					goPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-go gomod main/main()."}}},
				},
			},
		},
	}
	snapshotter := NativeRepositorySnapshotter{SCIP: config}

	_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: fileWithSizeSlice(pythonPath, goPath)},
		defaultCollectorTestEngine(t),
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if got, want := indexer.runLanguages, []string{"python", "go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SCIP run languages = %#v, want %#v", got, want)
	}
	if got, want := indexer.runRoots, []string{
		filepath.Join(repoRoot, "services", "api"),
		filepath.Join(repoRoot, "services", "worker"),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SCIP run roots = %#v, want %#v", got, want)
	}
	if got, _ := parsedFiles[0]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("python function_calls_scip = %#v, want SCIP supplement", parsedFiles[0]["function_calls_scip"])
	}
	if got, _ := parsedFiles[1]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("go function_calls_scip = %#v, want SCIP supplement", parsedFiles[1]["function_calls_scip"])
	}
}

func TestSCIPSnapshotRunsSameLanguagePackageSubtrees(t *testing.T) {
	repoRoot := t.TempDir()
	apiRoot := filepath.Join(repoRoot, "services", "api")
	jobsRoot := filepath.Join(repoRoot, "services", "jobs")
	apiPath := filepath.Join(apiRoot, "app.py")
	jobsPath := filepath.Join(jobsRoot, "worker.py")
	writeCollectorTestFile(t, filepath.Join(apiRoot, "pyproject.toml"), "[project]\nname = \"api\"\n")
	writeCollectorTestFile(t, filepath.Join(jobsRoot, "pyproject.toml"), "[project]\nname = \"jobs\"\n")
	writeCollectorTestFile(t, apiPath, "def main():\n    return helper()\n")
	writeCollectorTestFile(t, jobsPath, "def run():\n    return task()\n")

	indexer := &languagePathSCIPIndexer{available: map[string]bool{"python": true}}
	config := LoadSnapshotSCIPConfig(scipEnabledTestGetenv(nil))
	config.Workers = 1
	config.Indexer = indexer
	config.Parser = languagePathSCIPParser{
		resultsByRoot: map[string]parser.SCIPParseResult{
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
	snapshotter := NativeRepositorySnapshotter{SCIP: config}

	_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: fileWithSizeSlice(apiPath, jobsPath)},
		defaultCollectorTestEngine(t),
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if got, want := indexer.runLanguages, []string{"python", "python"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SCIP run languages = %#v, want %#v", got, want)
	}
	if got, want := indexer.runRoots, []string{apiRoot, jobsRoot}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SCIP run roots = %#v, want %#v", got, want)
	}
	if got, _ := parsedFiles[0]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("api function_calls_scip = %#v, want SCIP supplement", parsedFiles[0]["function_calls_scip"])
	}
	if got, _ := parsedFiles[1]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("jobs function_calls_scip = %#v, want SCIP supplement", parsedFiles[1]["function_calls_scip"])
	}
}

func TestSCIPSnapshotSameLanguageSubtreeFailurePreservesOtherRoots(t *testing.T) {
	repoRoot := t.TempDir()
	apiRoot := filepath.Join(repoRoot, "services", "api")
	jobsRoot := filepath.Join(repoRoot, "services", "jobs")
	apiPath := filepath.Join(apiRoot, "app.py")
	jobsPath := filepath.Join(jobsRoot, "worker.py")
	writeCollectorTestFile(t, filepath.Join(apiRoot, "pyproject.toml"), "[project]\nname = \"api\"\n")
	writeCollectorTestFile(t, filepath.Join(jobsRoot, "pyproject.toml"), "[project]\nname = \"jobs\"\n")
	writeCollectorTestFile(t, apiPath, "def main():\n    return helper()\n")
	writeCollectorTestFile(t, jobsPath, "def run():\n    return task()\n")

	indexer := &languagePathSCIPIndexer{
		available:       map[string]bool{"python": true},
		runErrorsByRoot: map[string]error{apiRoot: errors.New("indexer failed")},
	}
	config := LoadSnapshotSCIPConfig(scipEnabledTestGetenv(nil))
	config.Workers = 1
	config.Indexer = indexer
	config.Parser = languagePathSCIPParser{
		resultsByRoot: map[string]parser.SCIPParseResult{
			jobsRoot: {
				Files: map[string]map[string]any{
					jobsPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python jobs/run()."}}},
				},
			},
		},
	}
	snapshotter := NativeRepositorySnapshotter{SCIP: config}

	_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: fileWithSizeSlice(apiPath, jobsPath)},
		defaultCollectorTestEngine(t),
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if got, want := indexer.runRoots, []string{apiRoot, jobsRoot}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SCIP run roots = %#v, want %#v", got, want)
	}
	if _, ok := parsedFiles[0]["function_calls_scip"]; ok {
		t.Fatalf("api function_calls_scip = %#v, want native fallback only", parsedFiles[0]["function_calls_scip"])
	}
	if got, _ := parsedFiles[1]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("jobs function_calls_scip = %#v, want SCIP supplement", parsedFiles[1]["function_calls_scip"])
	}
}

func TestSCIPSnapshotLanguageSubtreeFallbackPreservesOtherLanguages(t *testing.T) {
	repoRoot := t.TempDir()
	pythonPath := filepath.Join(repoRoot, "services", "api", "app.py")
	goPath := filepath.Join(repoRoot, "services", "worker", "main.go")
	writeCollectorTestFile(t, pythonPath, "def main():\n    return helper()\n")
	writeCollectorTestFile(t, goPath, "package main\n\nfunc main() {}\n")

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-scip-subtree-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	indexer := &languagePathSCIPIndexer{available: map[string]bool{"python": true, "go": false}}
	config := LoadSnapshotSCIPConfig(scipEnabledTestGetenv(nil))
	config.Workers = 1
	config.Indexer = indexer
	config.Parser = languagePathSCIPParser{
		results: map[string]parser.SCIPParseResult{
			"python": {
				Files: map[string]map[string]any{
					pythonPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python app/main()."}}},
				},
			},
		},
	}
	snapshotter := NativeRepositorySnapshotter{
		Instruments: instruments,
		SCIP:        config,
	}

	_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: fileWithSizeSlice(pythonPath, goPath)},
		defaultCollectorTestEngine(t),
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if got, want := indexer.availabilityChecks, []string{"python", "go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SCIP availability checks = %#v, want %#v", got, want)
	}
	if got, want := indexer.runLanguages, []string{"python"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SCIP run languages = %#v, want %#v", got, want)
	}
	if got, _ := parsedFiles[0]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("python function_calls_scip = %#v, want SCIP supplement", parsedFiles[0]["function_calls_scip"])
	}
	if _, ok := parsedFiles[1]["function_calls_scip"]; ok {
		t.Fatalf("go function_calls_scip = %#v, want native fallback only", parsedFiles[1]["function_calls_scip"])
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got := collectorCounterValue(t, rm, "eshu_dp_scip_snapshot_attempts_total", map[string]string{
		"language": "python",
		"result":   "used",
	}); got != 1 {
		t.Fatalf("python SCIP used attempts = %d, want 1", got)
	}
	if got := collectorCounterValue(t, rm, "eshu_dp_scip_snapshot_attempts_total", map[string]string{
		"language": "go",
		"result":   "binary_unavailable",
	}); got != 1 {
		t.Fatalf("go SCIP binary_unavailable attempts = %d, want 1", got)
	}
}

type languagePathSCIPIndexer struct {
	available          map[string]bool
	availabilityChecks []string
	runLanguages       []string
	runRoots           []string
	runErrorsByRoot    map[string]error
}

func (i *languagePathSCIPIndexer) IsAvailable(language string) bool {
	i.availabilityChecks = append(i.availabilityChecks, language)
	return i.available[language]
}

func (i *languagePathSCIPIndexer) Run(_ context.Context, projectPath string, language string, outputDir string) (string, error) {
	i.runLanguages = append(i.runLanguages, language)
	i.runRoots = append(i.runRoots, projectPath)
	if err := i.runErrorsByRoot[projectPath]; err != nil {
		return "", err
	}
	return filepath.Join(outputDir, language+".scip"), nil
}

type languagePathSCIPParser struct {
	results       map[string]parser.SCIPParseResult
	resultsByRoot map[string]parser.SCIPParseResult
}

func (p languagePathSCIPParser) Parse(indexPath string, projectRoot string) (parser.SCIPParseResult, error) {
	if result, ok := p.resultsByRoot[projectRoot]; ok {
		return result, nil
	}
	language := filepath.Base(indexPath)
	language = language[:len(language)-len(filepath.Ext(language))]
	return p.results[language], nil
}
