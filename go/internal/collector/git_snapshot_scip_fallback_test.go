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
