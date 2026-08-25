// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perl_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPerlBasic(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "worker.pl")
	parsertest.WriteFile(
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

	parsertest.AssertNamedBucketContains(t, got, "classes", "Worker")
	parsertest.AssertNamedBucketContains(t, got, "imports", "App::Util")
	parsertest.AssertNamedBucketContains(t, got, "imports", "Exporter")
	parsertest.AssertNamedBucketContains(t, got, "functions", "new")
	parsertest.AssertNamedBucketContains(t, got, "functions", "run")
	parsertest.AssertNamedBucketContains(t, got, "functions", "public_action")
	parsertest.AssertNamedBucketContains(t, got, "functions", "_private_helper")
	parsertest.AssertNamedBucketContains(t, got, "variables", "task")
	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "full_name", "App::Util::build_task")
	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "full_name", "App::Util::execute")
}
