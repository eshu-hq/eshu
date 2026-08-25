// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package haskell_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDefaultEngineParsePathHaskellBasic(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Main.hs")
	writeTestFile(
		t,
		filePath,
		`module Main where

import Data.Text
import qualified Data.Map as M

data Worker = Worker { name :: String, age :: Int }

class Service a where
  perform :: a -> IO ()

instance Service Worker where
  perform worker = M.empty

main = putStrLn (show Worker { name = "test", age = 0 })
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if lang, ok := got["lang"].(string); !ok || lang != "haskell" {
		t.Fatalf("payload[lang] = %#v, want haskell", lang)
	}

	assertNamedBucketContains(t, got, "modules", "Main")
	assertNamedBucketContains(t, got, "imports", "Data.Text")
	assertNamedBucketContains(t, got, "imports", "Data.Map")
	assertNamedBucketContains(t, got, "classes", "Worker")
	assertNamedBucketContains(t, got, "functions", "main")
	assertNamedBucketContains(t, got, "functions", "perform")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "M.empty")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "putStrLn")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "show")
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
}

func assertNamedBucketContains(t *testing.T, payload map[string]any, key string, wantName string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name == wantName {
			return
		}
	}
	t.Fatalf("%s missing name %q in %#v", key, wantName, items)
}

func assertBucketContainsFieldValue(
	t *testing.T,
	payload map[string]any,
	key string,
	field string,
	wantValue string,
) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		value, _ := item[field].(string)
		if value == wantValue {
			return
		}
	}
	t.Fatalf("%s missing %s=%q in %#v", key, field, wantValue, items)
}
