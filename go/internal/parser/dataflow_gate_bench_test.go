// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dataflowGateBenchLanguages are the corpus fixture directories whose parsers
// implement value-flow lowering. The benchmark parses their real source files
// rather than a synthesized document, so the ratio it reports is the cost an
// operator actually pays for setting ESHU_EMIT_DATAFLOW on a repository of
// ordinary code.
// Every language the shared gate activates is measured. Java and C# emit the
// same buckets Go does (including the durable summaries/sources pair), and
// TypeScript routes through the JavaScript adapter but is a distinct fixture
// family with its own cost, so a sizing number quoted from Go alone would not
// hold for a JVM or .NET repository.
var dataflowGateBenchLanguages = map[string]string{
	"go":         ".go",
	"python":     ".py",
	"javascript": ".js",
	"typescript": ".ts",
	"java":       ".java",
	"csharp":     ".cs",
}

// dataflowGateBenchOptions mirrors what snapshotParserOptions
// (go/internal/collector/gitrepo/git_snapshot_parser_options.go) hands the parser on
// the real ingest path, because that is what decides how much value-flow work
// the gate actually triggers.
//
// RepositoryID and GoPackageImportPath are load-bearing, not decoration:
// several languages suppress their interprocedural ids without a repository
// id, and Go skips its durable dataflow_summaries and dataflow_sources
// buckets. Benchmarking without them measures a cheaper gate than any operator
// ever enables, and reports a ratio that is a lower bound rather than a cost.
func dataflowGateBenchOptions(emitDataflow bool, goImportPath string) Options {
	return Options{
		IndexSource:         true,
		VariableScope:       "all",
		RepositoryID:        "repository:dataflow-gate-bench",
		GoPackageImportPath: goImportPath,
		EmitDataflow:        emitDataflow,
	}
}

// dataflowGateBenchFiles collects the fixture files for one language.
func dataflowGateBenchFiles(b *testing.B, fixture, ext string) (string, []string) {
	b.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", fixture))
	if err != nil {
		b.Fatalf("resolve fixture root: %v", err)
	}
	var files []string
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ext) {
			return nil //nolint:nilerr // a missing or unreadable entry is skipped, not fatal
		}
		files = append(files, p)
		return nil
	})
	if walkErr != nil {
		b.Fatalf("walk %s: %v", root, walkErr)
	}
	if len(files) == 0 {
		b.Fatalf("no %s files under %s", ext, root)
	}
	return root, files
}

// BenchmarkDataflowGateEmissionCost reports what ESHU_EMIT_DATAFLOW costs, by
// parsing the same corpus fixtures with the gate off and on.
//
// Issue #5692 wired the gate into bootstrap-index and the ingester, which is
// what makes the cost reachable from the default stack at all. The gate stays
// opt-in, and this benchmark is the evidence for that decision: run it before
// proposing any change to the default.
//
//	go test ./internal/parser -run '^$' -bench BenchmarkDataflowGateEmissionCost -benchtime=10x
func BenchmarkDataflowGateEmissionCost(b *testing.B) {
	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v", err)
	}

	for fixture, ext := range dataflowGateBenchLanguages {
		root, files := dataflowGateBenchFiles(b, fixture+"_comprehensive", ext)

		goImportPath := ""
		if fixture == "go" {
			goImportPath = "github.com/eshu-hq/dataflow-gate-bench"
		}

		for _, gate := range []struct {
			name string
			opts Options
		}{
			{"gate=off", dataflowGateBenchOptions(false, goImportPath)},
			{"gate=on", dataflowGateBenchOptions(true, goImportPath)},
		} {
			b.Run(fixture+"/"+gate.name, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					for _, file := range files {
						if _, parseErr := engine.ParsePath(root, file, false, gate.opts); parseErr != nil {
							b.Fatalf("ParsePath(%s): %v", file, parseErr)
						}
					}
				}
			})
		}
	}
}
