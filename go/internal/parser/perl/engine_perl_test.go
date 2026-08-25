// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDefaultEngineParsePathPerlBasic(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "worker.pl")
	writeTestFile(
		t,
		filePath,
		`package App::Worker;
use App::Util;
use Exporter qw(import);
our @EXPORT_OK = qw(run public_action);

sub new {
  my ($class) = @_;
  return bless {}, $class;
}

sub run {
  my $task = App::Util::build_task();
  App::Util::execute($task);
}

sub public_action {}
sub _private_helper {}
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

	if lang, ok := got["lang"].(string); !ok || lang != "perl" {
		t.Fatalf("payload[lang] = %#v, want perl", lang)
	}

	assertNamedBucketContains(t, got, "classes", "Worker")
	assertNamedBucketContains(t, got, "imports", "App::Util")
	assertNamedBucketContains(t, got, "imports", "Exporter")
	assertNamedBucketContains(t, got, "functions", "new")
	assertNamedBucketContains(t, got, "functions", "run")
	assertNamedBucketContains(t, got, "functions", "public_action")
	assertNamedBucketContains(t, got, "functions", "_private_helper")
	assertNamedBucketContains(t, got, "variables", "task")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "App::Util::build_task")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "App::Util::execute")
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
