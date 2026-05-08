package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathTypeScriptAnnotatesBaseURLImportSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "handlers", "token.ts")
	writeTestFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "."
  }
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "server", "resources", "jwt.ts"), `export const encode = () => "";
`)
	writeTestFile(t, filePath, `import * as jwt from "server/resources/jwt";

export const post = () => jwt.encode();
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	jwtImport := findNamedBucketItem(t, got, "imports", "*")
	assertStringFieldValue(t, jwtImport, "source", "server/resources/jwt")
	assertStringFieldValue(t, jwtImport, "resolved_source", "server/resources/jwt.ts")
}

func TestDefaultEngineParsePathTypeScriptSkipsBaseURLImportWithoutTSConfig(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "handlers", "token.ts")
	writeTestFile(t, filepath.Join(repoRoot, "server", "resources", "jwt.ts"), `export const encode = () => "";
`)
	writeTestFile(t, filePath, `import * as jwt from "server/resources/jwt";

export const post = () => jwt.encode();
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	jwtImport := findNamedBucketItem(t, got, "imports", "*")
	if resolvedSource := jwtImport["resolved_source"]; resolvedSource != nil {
		t.Fatalf("resolved_source = %#v, want nil without local tsconfig baseUrl evidence", resolvedSource)
	}
}
