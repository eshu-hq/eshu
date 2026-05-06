package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathRubyEmitsFunctionArgsAndContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "worker.rb")
	writeTestFile(
		t,
		filePath,
		`module Comprehensive
  class Worker
    def perform(task, retries = 0)
      task.call
    end
  end
end
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

	functionItem := assertFunctionByName(t, got, "perform")
	if gotArgs, ok := functionItem["args"].([]string); !ok {
		t.Fatalf("functions[\"perform\"][\"args\"] = %T, want []string", functionItem["args"])
	} else if !reflect.DeepEqual(gotArgs, []string{"task", "retries"}) {
		t.Fatalf("functions[\"perform\"][\"args\"] = %#v, want %#v", gotArgs, []string{"task", "retries"})
	}
	assertStringFieldValue(t, functionItem, "context", "Worker")
	assertStringFieldValue(t, functionItem, "context_type", "class")
	assertStringFieldValue(t, functionItem, "class_context", "Worker")
}

func TestDefaultEngineParsePathRubyCapturesGenericDslAndMethodCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "dsl.rb")
	writeTestFile(
		t,
		filePath,
		`require_relative 'basic'

module Comprehensive
  module Cacheable
    def cache_method(method_name)
      original = instance_method(method_name)
      define_method(method_name) do |*args|
        @cache ||= {}
        key = [method_name, args]
        @cache[key] ||= original.bind(self).call(*args)
      end
    end
  end

  class Service
    include Cacheable
    attr_accessor :name, :debug

    def perform(task, retries = 0)
      task.call
      cache_method :perform
      retries = retries + 1
      @last_task = task
    end
  end
end
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

	assertNamedBucketContains(t, got, "imports", "basic")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "require_relative")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "attr_accessor")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "define_method")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "call")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "cache_method")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "task.call")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "original.bind")
}

func TestDefaultEngineParsePathRubyCapturesLocalAndInstanceAssignments(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "assignments.rb")
	writeTestFile(
		t,
		filePath,
		`class Worker
  def perform(task)
    retries = 1
    @last_task = task
    @cache ||= {}
  end
end
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

	assertNamedBucketContains(t, got, "variables", "retries")
	assertNamedBucketContains(t, got, "variables", "@last_task")
	assertNamedBucketContains(t, got, "variables", "@cache")
	retries := assertBucketItemByName(t, got, "variables", "retries")
	assertStringFieldValue(t, retries, "type", "1")
	assertStringFieldValue(t, retries, "context", "perform")
	assertStringFieldValue(t, retries, "context_type", "def")
	lastTask := assertBucketItemByName(t, got, "variables", "@last_task")
	assertStringFieldValue(t, lastTask, "type", "task")
	assertStringFieldValue(t, lastTask, "context", "perform")
	assertStringFieldValue(t, lastTask, "context_type", "def")
}

func TestDefaultEngineParsePathRubyEmitsRequireAndLoadImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "requires.rb")
	writeTestFile(
		t,
		filePath,
		`require_relative 'basic'
load 'support/bootstrap'
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

	assertNamedBucketContains(t, got, "imports", "basic")
	assertNamedBucketContains(t, got, "imports", "support/bootstrap")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "require_relative")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "load")
}

func TestDefaultEngineParsePathRubyCapturesChainedInvocationNames(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "chain.rb")
	writeTestFile(
		t,
		filePath,
		`module Comprehensive
  module Cacheable
    def cache_method(method_name)
      original = instance_method(method_name)

      define_method(method_name) do |*args|
        @cache ||= {}
        key = [method_name, args]
        @cache[key] ||= original.bind(self).call(*args)
      end
    end
  end
end
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

	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "original.bind.call")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "original.bind")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "call")
}

func TestDefaultEngineParsePathRubyDistinguishesSingletonAndDynamicDispatchMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "singleton.rb")
	writeTestFile(
		t,
		filePath,
		`class Service
  def self.build(name)
    new(name)
  end

  def perform(name)
    build(name)
  end
end

class Builder
  class << self
    def from_block(name)
      new(name)
    end
  end
end

class DSLBuilder
  def method_missing(name, *args)
    send(name, *args)
  end

  def respond_to_missing?(name, include_private = false)
    true
  end
end
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

	assertStringFieldValue(t, assertFunctionByName(t, got, "build"), "type", "singleton")
	assertStringFieldValue(t, assertFunctionByName(t, got, "perform"), "type", "instance")
	assertStringFieldValue(t, assertFunctionByName(t, got, "from_block"), "type", "singleton")
	assertStringFieldValue(t, assertFunctionByName(t, got, "method_missing"), "type", "dynamic_dispatch")
	assertStringFieldValue(t, assertFunctionByName(t, got, "respond_to_missing?"), "type", "dynamic_dispatch")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "send")
}
