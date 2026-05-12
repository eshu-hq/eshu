package haskell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseCapturesHaskellBuckets(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Main.hs", `module Main where
import Data.Text
data Worker = Worker
run task = result
  where
    result = task
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "modules", "Main")
	assertBucketName(t, payload, "imports", "Data.Text")
	assertBucketName(t, payload, "classes", "Worker")
	function := assertBucketName(t, payload, "functions", "run")
	if got := function["source"]; got != "run task = result" {
		t.Fatalf("functions[run][source] = %#v, want source line", got)
	}
	assertBucketName(t, payload, "variables", "result")
}

func TestParseCapturesHaskellDeadCodeRootsAndCalls(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "App.hs", `module Demo.App
  ( main
  , run
  , Worker(..)
  , Runner(..)
  ) where

import qualified Data.Text as T
import Data.List (nub, sort)

data Worker = Worker

class Runner a where
  runTask :: a -> IO ()

instance Runner Worker where
  runTask worker = run worker

main = run Worker
run worker = T.unpack (T.pack (show worker))
helper worker = sort (nub [worker])
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "modules", "Demo.App")
	importItem := assertBucketName(t, payload, "imports", "Data.Text")
	if got := importItem["alias"]; got != "T" {
		t.Fatalf("imports[Data.Text][alias] = %#v, want T", got)
	}
	assertParserStringSliceContains(t, assertBucketName(t, payload, "classes", "Worker"), "dead_code_root_kinds", "haskell.exported_type")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "classes", "Runner"), "dead_code_root_kinds", "haskell.exported_type")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "main"), "dead_code_root_kinds", "haskell.main_function")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "run"), "dead_code_root_kinds", "haskell.module_export")
	assertBucketMissingName(t, payload, "functions", "data")

	classMethod := assertBucketField(t, payload, "functions", "class_context", "Runner")
	if got := classMethod["name"]; got != "runTask" {
		t.Fatalf("class_context Runner function name = %#v, want runTask", got)
	}
	assertParserStringSliceContains(t, classMethod, "dead_code_root_kinds", "haskell.typeclass_method")

	instanceMethod := assertBucketField(t, payload, "functions", "class_context", "Runner Worker")
	if got := instanceMethod["name"]; got != "runTask" {
		t.Fatalf("class_context Runner Worker function name = %#v, want runTask", got)
	}
	assertParserStringSliceContains(t, instanceMethod, "dead_code_root_kinds", "haskell.instance_method")

	assertBucketField(t, payload, "function_calls", "full_name", "run")
	assertBucketField(t, payload, "function_calls", "full_name", "T.pack")
	assertBucketField(t, payload, "function_calls", "full_name", "sort")
	assertBucketMissingName(t, payload, "function_calls", "worker")
}

func TestParseCapturesHaskellContinuationCalls(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Bench.hs", `module Bench where

main = do
  env getImages $ \imgs ->
    bgroup "writers" $ mapMaybe (writerBench imgs doc . fst)
  bgroup "readers" $ mapMaybe (readerBench doc . fst)
  let versionOr action = if hasVersion then versionInfoCLI else action
  versionOr convert

versionInfoCLI = do
  scriptingEngine <- getEngine
  versionInfo getFeatures
              (Just $ T.unpack (engineName scriptingEngine))
              versionSuffix
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	for _, fullName := range []string{
		"env",
		"getImages",
		"writerBench",
		"readerBench",
		"versionInfoCLI",
		"versionInfo",
		"getFeatures",
		"T.unpack",
		"engineName",
		"versionSuffix",
	} {
		assertBucketField(t, payload, "function_calls", "full_name", fullName)
	}
}

func writeSource(t *testing.T, name string, source string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return path
}

func assertBucketName(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	t.Fatalf("payload[%q] missing name %q in %#v", bucket, name, items)
	return nil
}

func assertBucketField(t *testing.T, payload map[string]any, bucket string, field string, value any) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item[field] == value {
			return item
		}
	}
	t.Fatalf("payload[%q] missing %s=%#v in %#v", bucket, field, value, items)
	return nil
}

func assertBucketMissingName(t *testing.T, payload map[string]any, bucket string, name string) {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item["name"] == name {
			t.Fatalf("payload[%q] unexpectedly contains name %q in %#v", bucket, name, items)
		}
	}
}

func assertParserStringSliceContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	values, ok := item[field].([]string)
	if !ok {
		t.Fatalf("item[%s] = %T, want []string in %#v", field, item[field], item)
	}
	for _, got := range values {
		if got == want {
			return
		}
	}
	t.Fatalf("item[%s] missing %q in %#v", field, want, values)
}
