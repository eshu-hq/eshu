// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDefaultEngineParsePathJavaScriptPackageScriptRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "scripts": {
    "create:version": "node scripts/create-new-version.js",
    "generate": "tsx scripts/gen-client.ts"
  }
}
`)
	jsPath := filepath.Join(repoRoot, "scripts", "create-new-version.js")
	tsPath := filepath.Join(repoRoot, "scripts", "gen-client.ts")
	writeTestFile(t, jsPath, `const main = async () => {
  return "ok";
};

main();
`)
	writeTestFile(t, tsPath, `async function run() {
  return "ok";
}

run();
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	jsPayload, err := engine.ParsePath(repoRoot, jsPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(js) error = %v, want nil", err)
	}
	tsPayload, err := engine.ParsePath(repoRoot, tsPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(ts) error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, jsPayload, "main"),
		"dead_code_root_kinds",
		"javascript.node_package_script",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, tsPayload, "run"),
		"dead_code_root_kinds",
		"javascript.node_package_script",
	)
}
