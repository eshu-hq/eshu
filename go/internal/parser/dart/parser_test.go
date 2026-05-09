package dart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseCapturesDartBuckets(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "widget.dart", `import 'package:flutter/material.dart';
class HomePage {}
final counter = makeCounter();
Widget build() => Text('hi');
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "imports", "package:flutter/material.dart")
	assertBucketName(t, payload, "classes", "HomePage")
	function := assertBucketName(t, payload, "functions", "build")
	if got := function["source"]; got != "Widget build() => Text('hi');" {
		t.Fatalf("functions[build][source] = %#v, want source line", got)
	}
	assertBucketName(t, payload, "variables", "counter")
	assertBucketName(t, payload, "function_calls", "makeCounter")
}

func TestPreScanReturnsDartDeclarations(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "model.dart", `class Model {}
void run() {}
`)

	got, err := PreScan(path)
	if err != nil {
		t.Fatalf("PreScan() error = %v, want nil", err)
	}
	want := []string{"Model", "run"}
	if len(got) != len(want) {
		t.Fatalf("PreScan() = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("PreScan() = %#v, want %#v", got, want)
		}
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
