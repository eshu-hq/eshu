package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptRequireImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "commonjs.js")
	writeTestFile(
		t,
		filePath,
		`const express = require("express");
const { Router: ExpressRouter, json } = require("./router");

function buildRouter() {
  return express.Router();
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	expressImport := findNamedBucketItem(t, got, "imports", "*")
	assertStringFieldValue(t, expressImport, "alias", "express")
	assertStringFieldValue(t, expressImport, "source", "express")
	assertStringFieldValue(t, expressImport, "import_type", "require")

	routerImport := findNamedBucketItem(t, got, "imports", "Router")
	assertStringFieldValue(t, routerImport, "alias", "ExpressRouter")
	assertStringFieldValue(t, routerImport, "source", "./router")
	assertStringFieldValue(t, routerImport, "import_type", "require")

	jsonImport := findNamedBucketItem(t, got, "imports", "json")
	assertStringFieldValue(t, jsonImport, "source", "./router")
	assertStringFieldValue(t, jsonImport, "import_type", "require")
}

func TestDefaultEngineParsePathJavaScriptRequireTemplateLiteralInterpolationIsSkipped(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "dynamic_commonjs.js")
	writeTestFile(
		t,
		filePath,
		"const name = getName();\nconst router = require(`./${name}/router`);\n",
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	imports, ok := got["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	}
	if len(imports) != 0 {
		t.Fatalf("imports = %#v, want no imports for runtime-dependent require path", imports)
	}
}
