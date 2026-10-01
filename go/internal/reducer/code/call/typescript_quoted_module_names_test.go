// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestExtractCodeCallRowsResolvesStringLiteralModuleNames drives the whole
// parse-then-reduce path for ES2022 string-literal module export names (#7461):
// a consumer imports `'encode'` (quoted) from a barrel that re-exports it under a
// quoted alias, and the call must reach the implementation, the same as the
// identifier spelling would.
func TestExtractCodeCallRowsResolvesStringLiteralModuleNames(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app", "run.ts")
	barrelPath := filepath.Join(repoRoot, "app", "barrel.ts")
	calleePath := filepath.Join(repoRoot, "app", "impl.ts")
	writeReducerTestFile(t, callerPath, `import { 'public encode' as direct } from "./barrel";
import { 'encode' as plain } from "./impl";

export const run = () => {
  const first = direct(1);
  const second = plain(2);
  return [first, second];
};
`)
	writeReducerTestFile(t, barrelPath, `export { 'encode' as 'public encode' } from "./impl";
`)
	writeReducerTestFile(t, calleePath, `export const encode = (data: number) => String(data);
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	paths := []string{callerPath, barrelPath, calleePath}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, paths)
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	envelopes := []facts.Envelope{{
		FactKind: "repository",
		Payload:  map[string]any{"repo_id": "repo-ts", "imports_map": importsMap},
	}}
	for _, path := range paths {
		payload, err := engine.ParsePath(repoRoot, path, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%s) error = %v, want nil", path, err)
		}
		switch path {
		case callerPath:
			assignReducerTestFunctionUID(t, payload, "run", "content-entity:ts-run")
		case calleePath:
			assignReducerTestFunctionUID(t, payload, "encode", "content-entity:ts-encode")
		}
		envelopes = append(envelopes, facts.Envelope{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-ts",
				"relative_path":    reducerTestRelativePath(t, repoRoot, path),
				"parsed_file_data": payload,
			},
		})
	}

	_, rows := ExtractRows(envelopes)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d; rows=%#v", got, want, rows)
	}
	for _, row := range rows {
		if got, want := row["callee_entity_id"], "content-entity:ts-encode"; got != want {
			t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
		}
		if got, want := row["callee_file"], "app/impl.ts"; got != want {
			t.Fatalf("callee_file = %#v, want %#v; rows=%#v", got, want, rows)
		}
	}
}
