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
