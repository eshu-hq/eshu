// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestFileParseDurationSecondsUsesSeconds(t *testing.T) {
	t.Parallel()

	startedAt := time.Now().Add(-1500 * time.Millisecond)

	got := fileParseDurationSeconds(startedAt)
	if got < 1.0 || got > 2.0 {
		t.Fatalf("fileParseDurationSeconds() = %f, want seconds near 1.5", got)
	}
}

func TestMergeSCIPSupplementAttachesCallsAndPreservesNativeRoots(t *testing.T) {
	t.Parallel()

	parsed := map[string]any{
		"functions": []map[string]any{
			{
				"name":                 "ServePayments",
				"dead_code_root_kinds": []string{"go.net_http_handler_registration"},
			},
		},
	}
	scipCalls := []map[string]any{{"callee_symbol": "scip-go gomod example/ServePayments()."}}
	supplement := map[string]any{
		"functions": []map[string]any{
			{
				"name": "ServePayments",
			},
		},
		"function_calls_scip": scipCalls,
	}

	mergeSCIPSupplement(parsed, supplement)

	gotCalls, ok := parsed["function_calls_scip"].([]map[string]any)
	if !ok || len(gotCalls) != 1 {
		t.Fatalf("function_calls_scip = %#v, want one attached SCIP call", parsed["function_calls_scip"])
	}
	functions, ok := parsed["functions"].([]map[string]any)
	if !ok || len(functions) != 1 {
		t.Fatalf("parsed[functions] = %#v, want one merged function", parsed["functions"])
	}
	got, ok := functions[0]["dead_code_root_kinds"].([]string)
	if !ok {
		t.Fatalf("dead_code_root_kinds = %T, want []string", functions[0]["dead_code_root_kinds"])
	}
	if len(got) != 1 || got[0] != "go.net_http_handler_registration" {
		t.Fatalf("dead_code_root_kinds = %#v, want %#v", got, []string{"go.net_http_handler_registration"})
	}
}

func TestLoadSnapshotSCIPConfigDefaultsDisabledForTopLanguageList(t *testing.T) {
	t.Parallel()

	config := LoadSnapshotSCIPConfig(func(string) string {
		return ""
	})

	if config.Enabled {
		t.Fatalf("Enabled = true, want default SCIP disabled")
	}
	wantLanguages := []string{"python", "typescript", "javascript", "go", "rust", "java", "cpp", "c"}
	if !reflect.DeepEqual(config.Languages, wantLanguages) {
		t.Fatalf("Languages = %#v, want %#v", config.Languages, wantLanguages)
	}
}

func TestLoadSnapshotSCIPConfigExplicitEnableValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"1", "true", "yes", "on"} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			config := LoadSnapshotSCIPConfig(func(key string) string {
				if key == "SCIP_INDEXER" {
					return raw
				}
				return ""
			})

			if !config.Enabled {
				t.Fatalf("Enabled = false, want SCIP_INDEXER=%q to enable SCIP", raw)
			}
		})
	}
}

func TestLoadSnapshotSCIPConfigExplicitDisablePreservesLanguageNarrowing(t *testing.T) {
	t.Parallel()

	config := LoadSnapshotSCIPConfig(func(key string) string {
		switch key {
		case "SCIP_INDEXER":
			return "false"
		case "SCIP_LANGUAGES":
			return "go, python"
		default:
			return ""
		}
	})

	if config.Enabled {
		t.Fatalf("Enabled = true, want explicit SCIP_INDEXER=false to disable SCIP")
	}
	wantLanguages := []string{"go", "python"}
	if !reflect.DeepEqual(config.Languages, wantLanguages) {
		t.Fatalf("Languages = %#v, want %#v", config.Languages, wantLanguages)
	}
}

func TestSCIPSnapshotExplicitEnableUsesSCIPWhenBinaryAvailable(t *testing.T) {
	repoRoot := t.TempDir()
	appPath := filepath.Join(repoRoot, "app.py")
	writeCollectorTestFile(t, appPath, "def main():\n    return 1\n")

	indexer := &recordingSCIPIndexer{available: true}
	config := LoadSnapshotSCIPConfig(func(key string) string {
		if key == "SCIP_INDEXER" {
			return "true"
		}
		return ""
	})
	config.Indexer = indexer
	config.Parser = fakeSCIPParser{
		result: parser.SCIPParseResult{
			Files: map[string]map[string]any{
				appPath: {
					"path":                appPath,
					"lang":                "python",
					"function_calls_scip": []map[string]any{{"callee_symbol": "scip-python python app/main()."}},
				},
			},
		},
	}
	snapshotter := NativeRepositorySnapshotter{SCIP: config}

	_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: fileWithSizeSlice(appPath)},
		defaultCollectorTestEngine(t),
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if got := indexer.runLanguages; !reflect.DeepEqual(got, []string{"python"}) {
		t.Fatalf("SCIP run languages = %#v, want python", got)
	}
	if got, _ := parsedFiles[0]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("function_calls_scip = %#v, want explicit SCIP supplement", parsedFiles[0]["function_calls_scip"])
	}
}

func TestSCIPSnapshotExplicitDisableFallsBackToNative(t *testing.T) {
	repoRoot := t.TempDir()
	appPath := filepath.Join(repoRoot, "app.py")
	writeCollectorTestFile(t, appPath, "def main():\n    return 1\n")

	indexer := &recordingSCIPIndexer{available: true}
	config := LoadSnapshotSCIPConfig(func(key string) string {
		if key == "SCIP_INDEXER" {
			return "off"
		}
		return ""
	})
	config.Indexer = indexer
	config.Parser = fakeSCIPParser{
		result: parser.SCIPParseResult{
			Files: map[string]map[string]any{
				appPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "unused"}}},
			},
		},
	}
	snapshotter := NativeRepositorySnapshotter{SCIP: config}

	_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: fileWithSizeSlice(appPath)},
		defaultCollectorTestEngine(t),
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if len(indexer.runLanguages) != 0 {
		t.Fatalf("SCIP run languages = %#v, want no SCIP run when disabled", indexer.runLanguages)
	}
	if _, ok := parsedFiles[0]["function_calls_scip"]; ok {
		t.Fatalf("function_calls_scip = %#v, want native-only output when SCIP disabled", parsedFiles[0]["function_calls_scip"])
	}
}

func TestSCIPSnapshotUnavailableBinaryFallsBackToNative(t *testing.T) {
	repoRoot := t.TempDir()
	appPath := filepath.Join(repoRoot, "app.py")
	writeCollectorTestFile(t, appPath, "def main():\n    return 1\n")

	indexer := &recordingSCIPIndexer{available: false}
	config := LoadSnapshotSCIPConfig(scipEnabledTestGetenv(nil))
	config.Indexer = indexer
	config.Parser = fakeSCIPParser{
		result: parser.SCIPParseResult{
			Files: map[string]map[string]any{
				appPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "unused"}}},
			},
		},
	}
	snapshotter := NativeRepositorySnapshotter{SCIP: config}

	_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: fileWithSizeSlice(appPath)},
		defaultCollectorTestEngine(t),
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if got := indexer.availabilityChecks; !reflect.DeepEqual(got, []string{"python"}) {
		t.Fatalf("SCIP availability checks = %#v, want python", got)
	}
	if len(indexer.runLanguages) != 0 {
		t.Fatalf("SCIP run languages = %#v, want no run when binary unavailable", indexer.runLanguages)
	}
	if _, ok := parsedFiles[0]["function_calls_scip"]; ok {
		t.Fatalf("function_calls_scip = %#v, want native-only output without SCIP binary", parsedFiles[0]["function_calls_scip"])
	}
}

func TestSCIPSnapshotLanguagesNarrowDominantSelection(t *testing.T) {
	repoRoot := t.TempDir()
	pythonPath := filepath.Join(repoRoot, "app.py")
	goPath := filepath.Join(repoRoot, "main.go")
	writeCollectorTestFile(t, pythonPath, "def main():\n    return 1\n")
	writeCollectorTestFile(t, goPath, "package main\n\nfunc main() {}\n")

	indexer := &recordingSCIPIndexer{available: true}
	config := LoadSnapshotSCIPConfig(scipEnabledTestGetenv(map[string]string{"SCIP_LANGUAGES": "go"}))
	config.Indexer = indexer
	config.Parser = fakeSCIPParser{
		result: parser.SCIPParseResult{
			Files: map[string]map[string]any{
				goPath: {"function_calls_scip": []map[string]any{{"callee_symbol": "scip-go gomod main/main()."}}},
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

	if got := indexer.runLanguages; !reflect.DeepEqual(got, []string{"go"}) {
		t.Fatalf("SCIP run languages = %#v, want narrowed go selection", got)
	}
	if _, ok := parsedFiles[0]["function_calls_scip"]; ok {
		t.Fatalf("python function_calls_scip = %#v, want absent when SCIP_LANGUAGES narrows to go", parsedFiles[0]["function_calls_scip"])
	}
	if got, _ := parsedFiles[1]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("go function_calls_scip = %#v, want SCIP supplement", parsedFiles[1]["function_calls_scip"])
	}
}

func TestSCIPSnapshotKeepsSelectedFilesMissingFromIndex(t *testing.T) {
	repoRoot := t.TempDir()
	appPath := filepath.Join(repoRoot, "app.py")
	helperPath := filepath.Join(repoRoot, "helper.py")
	writeCollectorTestFile(t, appPath, "def main():\n    return helper()\n")
	writeCollectorTestFile(t, helperPath, "def helper():\n    return 1\n")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-scip-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	scipCalls := []map[string]any{
		{
			"caller_symbol": "scip-python python app/main().",
			"callee_symbol": "scip-python python helper/helper().",
		},
	}
	config := LoadSnapshotSCIPConfig(scipEnabledTestGetenv(nil))
	config.Indexer = &recordingSCIPIndexer{available: true}
	config.Parser = fakeSCIPParser{
		result: parser.SCIPParseResult{
			Files: map[string]map[string]any{
				appPath: {
					"path":                appPath,
					"lang":                "python",
					"is_dependency":       false,
					"functions":           []map[string]any{},
					"classes":             []map[string]any{},
					"variables":           []map[string]any{},
					"imports":             []map[string]any{},
					"function_calls_scip": scipCalls,
				},
			},
		},
	}
	snapshotter := NativeRepositorySnapshotter{
		Instruments: instruments,
		SCIP:        config,
	}

	shapeFiles, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: fileWithSizeSlice(appPath, helperPath)},
		engine,
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if len(shapeFiles) != 2 {
		t.Fatalf("len(shapeFiles) = %d, want 2", len(shapeFiles))
	}
	if got, want := []string{shapeFiles[0].Path, shapeFiles[1].Path}, []string{"app.py", "helper.py"}; !collectorStringSlicesEqual(got, want) {
		t.Fatalf("shape file paths = %#v, want %#v", got, want)
	}
	if len(parsedFiles) != 2 {
		t.Fatalf("len(parsedFiles) = %d, want 2", len(parsedFiles))
	}
	if got, _ := parsedFiles[0]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("app.py function_calls_scip = %#v, want one SCIP call", parsedFiles[0]["function_calls_scip"])
	}
	if _, ok := parsedFiles[1]["function_calls_scip"]; ok {
		t.Fatalf("helper.py function_calls_scip = %#v, want absent for file missing from SCIP index", parsedFiles[1]["function_calls_scip"])
	}
	functions, _ := parsedFiles[1]["functions"].([]map[string]any)
	if len(functions) != 1 || functions[0]["name"] != "helper" {
		t.Fatalf("helper.py functions = %#v, want native parser output for helper", parsedFiles[1]["functions"])
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got := collectorCounterValue(t, rm, "eshu_dp_files_parsed_total", map[string]string{
		"status": "succeeded",
	}); got != 2 {
		t.Fatalf("eshu_dp_files_parsed_total{status=succeeded} = %d, want 2", got)
	}
	if got := scipHistogramCount(t, rm, "eshu_dp_file_parse_duration_seconds", map[string]string{
		"language": "python",
	}); got != 2 {
		t.Fatalf("eshu_dp_file_parse_duration_seconds{language=python} count = %d, want 2", got)
	}
}

func defaultCollectorTestEngine(t *testing.T) *parser.Engine {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	return engine
}

func scipEnabledTestGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		if key == "SCIP_INDEXER" {
			return "true"
		}
		if values == nil {
			return ""
		}
		return values[key]
	}
}

type recordingSCIPIndexer struct {
	available          bool
	runErr             error
	availabilityChecks []string
	runLanguages       []string
}

func (i *recordingSCIPIndexer) IsAvailable(language string) bool {
	i.availabilityChecks = append(i.availabilityChecks, language)
	return i.available
}

func (i *recordingSCIPIndexer) Run(_ context.Context, _ string, language string, _ string) (string, error) {
	i.runLanguages = append(i.runLanguages, language)
	if i.runErr != nil {
		return "", i.runErr
	}
	return "fake-index.scip", nil
}

type fakeSCIPParser struct {
	result parser.SCIPParseResult
	err    error
}

func (p fakeSCIPParser) Parse(string, string) (parser.SCIPParseResult, error) {
	if p.err != nil {
		return parser.SCIPParseResult{}, p.err
	}
	return p.result, nil
}

func scipHistogramCount(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
) uint64 {
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
					return dp.Count
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}
