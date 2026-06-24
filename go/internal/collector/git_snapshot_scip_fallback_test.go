// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSCIPSnapshotConcurrentParseMergesSCIPSupplement(t *testing.T) {
	repoRoot := t.TempDir()
	appPath := filepath.Join(repoRoot, "app.py")
	helperPath := filepath.Join(repoRoot, "helper.py")
	writeCollectorTestFile(t, appPath, "def main():\n    return helper()\n")
	writeCollectorTestFile(t, helperPath, "def helper():\n    return 1\n")

	indexer := &recordingSCIPIndexer{available: true}
	config := LoadSnapshotSCIPConfig(func(string) string {
		return ""
	})
	config.Indexer = indexer
	config.Parser = fakeSCIPParser{
		result: parser.SCIPParseResult{
			Files: map[string]map[string]any{
				appPath: {
					"function_calls_scip": []map[string]any{{
						"callee_symbol": "scip-python python helper/helper().",
					}},
				},
			},
		},
	}
	snapshotter := NativeRepositorySnapshotter{
		ParseWorkers: 2,
		SCIP:         config,
	}

	shapeFiles, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		discovery.RepoFileSet{Files: []string{appPath, helperPath}},
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
	if got, want := len(shapeFiles), 2; got != want {
		t.Fatalf("len(shapeFiles) = %d, want %d", got, want)
	}
	if got, _ := parsedFiles[0]["function_calls_scip"].([]map[string]any); len(got) != 1 {
		t.Fatalf("function_calls_scip = %#v, want SCIP supplement in concurrent parse path", parsedFiles[0]["function_calls_scip"])
	}
}

func TestSCIPSnapshotFallbackLogsBoundedReason(t *testing.T) {
	tests := []struct {
		name      string
		indexer   *recordingSCIPIndexer
		parser    fakeSCIPParser
		wantLog   string
		wantClass string
	}{
		{
			name:      "binary unavailable",
			indexer:   &recordingSCIPIndexer{available: false},
			wantLog:   `"reason":"binary_unavailable"`,
			wantClass: `"failure_class":"scip_binary_unavailable"`,
		},
		{
			name:      "indexer failed",
			indexer:   &recordingSCIPIndexer{available: true, runErr: errors.New("indexer failed")},
			wantLog:   `"reason":"indexer_failed"`,
			wantClass: `"failure_class":"scip_indexer_failed"`,
		},
		{
			name:      "parse failed",
			indexer:   &recordingSCIPIndexer{available: true},
			parser:    fakeSCIPParser{err: errors.New("parse failed")},
			wantLog:   `"reason":"parse_failed"`,
			wantClass: `"failure_class":"scip_parse_failed"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			appPath := filepath.Join(repoRoot, "app.py")
			writeCollectorTestFile(t, appPath, "def main():\n    return 1\n")

			config := LoadSnapshotSCIPConfig(func(string) string {
				return ""
			})
			config.Indexer = tt.indexer
			config.Parser = tt.parser
			var logs bytes.Buffer
			snapshotter := NativeRepositorySnapshotter{
				Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
				SCIP:   config,
			}

			_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
				context.Background(),
				repoRoot,
				discovery.RepoFileSet{Files: []string{appPath}},
				defaultCollectorTestEngine(t),
				"commit-sha",
				false,
				parser.GoPackageSemanticRoots{},
				"repo-alpha",
			)
			if err != nil {
				t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
			}
			if _, ok := parsedFiles[0]["function_calls_scip"]; ok {
				t.Fatalf("function_calls_scip = %#v, want native-only fallback", parsedFiles[0]["function_calls_scip"])
			}
			logOutput := logs.String()
			for _, want := range []string{
				`"msg":"SCIP snapshot fallback to native parser"`,
				`"language":"python"`,
				tt.wantLog,
				tt.wantClass,
			} {
				if !strings.Contains(logOutput, want) {
					t.Fatalf("fallback log missing %s in %s", want, logOutput)
				}
			}
		})
	}
}

func TestSCIPSnapshotRecordsAttemptResults(t *testing.T) {
	tests := []struct {
		name         string
		configure    func(t *testing.T, appPath string) SnapshotSCIPConfig
		wantLanguage string
		wantResult   string
	}{
		{
			name: "disabled",
			configure: func(t *testing.T, _ string) SnapshotSCIPConfig {
				t.Helper()
				return LoadSnapshotSCIPConfig(func(key string) string {
					if key == "SCIP_INDEXER" {
						return "false"
					}
					return ""
				})
			},
			wantLanguage: "unknown",
			wantResult:   "disabled",
		},
		{
			name: "no supported language",
			configure: func(t *testing.T, _ string) SnapshotSCIPConfig {
				t.Helper()
				config := LoadSnapshotSCIPConfig(func(string) string {
					return ""
				})
				config.Languages = []string{"go"}
				return config
			},
			wantLanguage: "unknown",
			wantResult:   "no_supported_language",
		},
		{
			name: "binary unavailable",
			configure: func(t *testing.T, _ string) SnapshotSCIPConfig {
				t.Helper()
				config := LoadSnapshotSCIPConfig(func(string) string {
					return ""
				})
				config.Indexer = &recordingSCIPIndexer{available: false}
				return config
			},
			wantLanguage: "python",
			wantResult:   "binary_unavailable",
		},
		{
			name: "indexer failed",
			configure: func(t *testing.T, _ string) SnapshotSCIPConfig {
				t.Helper()
				config := LoadSnapshotSCIPConfig(func(string) string {
					return ""
				})
				config.Indexer = &recordingSCIPIndexer{available: true, runErr: errors.New("indexer failed")}
				return config
			},
			wantLanguage: "python",
			wantResult:   "indexer_failed",
		},
		{
			name: "parse failed",
			configure: func(t *testing.T, _ string) SnapshotSCIPConfig {
				t.Helper()
				config := LoadSnapshotSCIPConfig(func(string) string {
					return ""
				})
				config.Indexer = &recordingSCIPIndexer{available: true}
				config.Parser = fakeSCIPParser{err: errors.New("parse failed")}
				return config
			},
			wantLanguage: "python",
			wantResult:   "parse_failed",
		},
		{
			name: "empty result",
			configure: func(t *testing.T, _ string) SnapshotSCIPConfig {
				t.Helper()
				config := LoadSnapshotSCIPConfig(func(string) string {
					return ""
				})
				config.Indexer = &recordingSCIPIndexer{available: true}
				config.Parser = fakeSCIPParser{result: parser.SCIPParseResult{Files: map[string]map[string]any{}}}
				return config
			},
			wantLanguage: "python",
			wantResult:   "empty_result",
		},
		{
			name: "used",
			configure: func(t *testing.T, appPath string) SnapshotSCIPConfig {
				t.Helper()
				config := LoadSnapshotSCIPConfig(func(string) string {
					return ""
				})
				config.Indexer = &recordingSCIPIndexer{available: true}
				config.Parser = fakeSCIPParser{
					result: parser.SCIPParseResult{
						Files: map[string]map[string]any{
							appPath: {
								"function_calls_scip": []map[string]any{{
									"callee_symbol": "scip-python python app/main().",
								}},
							},
						},
					},
				}
				return config
			},
			wantLanguage: "python",
			wantResult:   "used",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			appPath := filepath.Join(repoRoot, "app.py")
			writeCollectorTestFile(t, appPath, "def main():\n    return 1\n")

			metricReader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
			instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-scip-attempt-test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v, want nil", err)
			}
			snapshotter := NativeRepositorySnapshotter{
				Instruments: instruments,
				SCIP:        tt.configure(t, appPath),
			}

			_, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
				context.Background(),
				repoRoot,
				discovery.RepoFileSet{Files: []string{appPath}},
				defaultCollectorTestEngine(t),
				"commit-sha",
				false,
				parser.GoPackageSemanticRoots{},
				"repo-alpha",
			)
			if err != nil {
				t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
			}
			if len(parsedFiles) != 1 {
				t.Fatalf("len(parsedFiles) = %d, want 1", len(parsedFiles))
			}

			var rm metricdata.ResourceMetrics
			if err := metricReader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v, want nil", err)
			}
			if got := collectorCounterValue(t, rm, "eshu_dp_scip_snapshot_attempts_total", map[string]string{
				"language": tt.wantLanguage,
				"result":   tt.wantResult,
			}); got != 1 {
				t.Fatalf(
					"eshu_dp_scip_snapshot_attempts_total{language=%q,result=%q} = %d, want 1",
					tt.wantLanguage,
					tt.wantResult,
					got,
				)
			}
		})
	}
}
