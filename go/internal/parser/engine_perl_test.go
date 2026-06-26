// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
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
