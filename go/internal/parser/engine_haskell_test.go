// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
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
