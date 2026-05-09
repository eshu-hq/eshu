package ruby

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseCapturesRubyContextAndCalls(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "worker.rb", `require_relative 'basic'
module Comprehensive
  class Worker < BaseWorker
    include Cacheable
    def perform(task, retries = 0)
      task.call
      @last_task = task
    end
  end
end
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "imports", "basic")
	assertBucketName(t, payload, "modules", "Comprehensive")
	classItem := assertBucketName(t, payload, "classes", "Worker")
	if got := classItem["bases"]; !reflect.DeepEqual(got, []string{"BaseWorker"}) {
		t.Fatalf("classes[Worker][bases] = %#v, want BaseWorker", got)
	}
	function := assertBucketName(t, payload, "functions", "perform")
	if got := function["source"]; got != "    def perform(task, retries = 0)" {
		t.Fatalf("functions[perform][source] = %#v, want source line", got)
	}
	if got := function["args"]; !reflect.DeepEqual(got, []string{"task", "retries"}) {
		t.Fatalf("functions[perform][args] = %#v, want task/retries", got)
	}
	if got := function["class_context"]; got != "Worker" {
		t.Fatalf("functions[perform][class_context] = %#v, want Worker", got)
	}
	assertBucketName(t, payload, "variables", "@last_task")
	assertBucketField(t, payload, "module_inclusions", "module", "Cacheable")
	assertBucketField(t, payload, "function_calls", "full_name", "task.call")
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

	return assertBucketField(t, payload, bucket, "name", name)
}

func assertBucketField(t *testing.T, payload map[string]any, bucket string, field string, value string) map[string]any {
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
	t.Fatalf("payload[%q] missing %s=%q in %#v", bucket, field, value, items)
	return nil
}
