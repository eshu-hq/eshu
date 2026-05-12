package perl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseCapturesPerlBuckets(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "worker.pl", `package App::Worker;
use App::Util;
sub run {
  my $task = build_task();
  App::Util::execute($task);
}
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "classes", "Worker")
	assertBucketName(t, payload, "imports", "App::Util")
	function := assertBucketName(t, payload, "functions", "run")
	if got := function["source"]; got != "sub run {" {
		t.Fatalf("functions[run][source] = %#v, want source line", got)
	}
	assertBucketName(t, payload, "variables", "task")
	assertBucketName(t, payload, "function_calls", "build_task")
	assertBucketName(t, payload, "function_calls", "App::Util::execute")
}

func TestParseMarksPerlDeadCodeRoots(t *testing.T) {
	t.Parallel()

	path := writeSource(t, filepath.Join("bin", "controller.pl"), `package App::Controller;
use strict;
use warnings;
use Exporter qw(import);
our @EXPORT_OK = qw(public_action helper_action);
our @EXPORT = qw(default_action);

BEGIN {
  setup_environment();
}

sub new {
  my ($class) = @_;
  return bless {}, $class;
}

sub main {
  public_action();
}

sub public_action {}
sub helper_action {}
sub default_action {}

sub AUTOLOAD {}
sub DESTROY {}

sub _private_helper {}
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertBucketName(t, payload, "classes", "Controller"), "dead_code_root_kinds", "perl.package_namespace")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "new"), "dead_code_root_kinds", "perl.constructor")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "main"), "dead_code_root_kinds", "perl.script_entrypoint")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "public_action"), "dead_code_root_kinds", "perl.exported_subroutine")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "helper_action"), "dead_code_root_kinds", "perl.exported_subroutine")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "default_action"), "dead_code_root_kinds", "perl.exported_subroutine")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "BEGIN"), "dead_code_root_kinds", "perl.special_block")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "AUTOLOAD"), "dead_code_root_kinds", "perl.autoload_subroutine")
	assertParserStringSliceContains(t, assertBucketName(t, payload, "functions", "DESTROY"), "dead_code_root_kinds", "perl.destroy_subroutine")
	private := assertBucketName(t, payload, "functions", "_private_helper")
	if private["dead_code_root_kinds"] != nil {
		t.Fatalf("_private_helper dead_code_root_kinds = %#v, want nil", private["dead_code_root_kinds"])
	}
}

func TestParseKeepsExporterRootsPackageScoped(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "multi_package.pm", `package App::Public;
use Exporter qw(import);
our @EXPORT_OK = qw(shared_name public_only);

sub shared_name {}
sub public_only {}

package App::Internal;

sub shared_name {}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	publicShared := assertFunctionByNameAndContext(t, payload, "shared_name", "Public")
	assertParserStringSliceContains(t, publicShared, "dead_code_root_kinds", "perl.exported_subroutine")
	publicOnly := assertFunctionByNameAndContext(t, payload, "public_only", "Public")
	assertParserStringSliceContains(t, publicOnly, "dead_code_root_kinds", "perl.exported_subroutine")
	internalShared := assertFunctionByNameAndContext(t, payload, "shared_name", "Internal")
	if internalShared["dead_code_root_kinds"] != nil {
		t.Fatalf("internal shared_name dead_code_root_kinds = %#v, want nil", internalShared["dead_code_root_kinds"])
	}
}

func writeSource(t *testing.T, name string, source string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
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

func assertFunctionByNameAndContext(t *testing.T, payload map[string]any, name string, context string) map[string]any {
	t.Helper()

	items, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[functions] = %T, want []map[string]any", payload["functions"])
	}
	for _, item := range items {
		if item["name"] == name && item["class_context"] == context {
			return item
		}
	}
	t.Fatalf("payload[functions] missing name %q with class_context %q in %#v", name, context, items)
	return nil
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
