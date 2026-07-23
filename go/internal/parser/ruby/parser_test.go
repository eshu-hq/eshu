// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// TestParseEmitsQualifiedBasesForClassSuperclass pins issue #5376 D0: the Ruby
// parser emits qualified_bases (the full, module-qualified base) additively
// next to the last-segment bases fact. The reducer's repo-wide code-root
// verdict builder needs the qualification — bases collapses
// "ActionController::Base" to "Base", which would conflate a real controller
// base with an unrelated class sharing the same last segment.
func TestParseEmitsQualifiedBasesForClassSuperclass(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "orders_controller.rb", `class OrdersController < ActionController::Base
  def index
    true
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	classItem := assertBucketName(t, payload, "classes", "OrdersController")
	if got := classItem["bases"]; !reflect.DeepEqual(got, []string{"Base"}) {
		t.Fatalf("classes[OrdersController][bases] = %#v, want [Base] (last-segment)", got)
	}
	if got := classItem["qualified_bases"]; !reflect.DeepEqual(got, []string{"ActionController::Base"}) {
		t.Fatalf("classes[OrdersController][qualified_bases] = %#v, want [ActionController::Base]", got)
	}
}

// TestParseEmitsQualifiedNameForNamespacedClass pins #5376 F3: the parser emits
// qualified_name for a namespaced class in BOTH the nested `module Admin; class
// Base` spelling and the compact `class Admin::Base` spelling, while name stays
// the last segment. The reducer keys its repo-wide registry on qualified_name so
// a base reference like "Admin::Base" resolves to the right class.
func TestParseEmitsQualifiedNameForNamespacedClass(t *testing.T) {
	t.Parallel()

	t.Run("nested module spelling", func(t *testing.T) {
		t.Parallel()
		path := writeSource(t, "nested.rb", `module Admin
  class Base < ActionController::Base
  end
end
`)
		payload, err := Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
		classItem := assertBucketName(t, payload, "classes", "Base")
		if got := classItem["qualified_name"]; got != "Admin::Base" {
			t.Fatalf("classes[Base][qualified_name] = %#v, want Admin::Base", got)
		}
		if got := classItem["qualified_bases"]; !reflect.DeepEqual(got, []string{"ActionController::Base"}) {
			t.Fatalf("classes[Base][qualified_bases] = %#v, want [ActionController::Base]", got)
		}
	})

	t.Run("compact spelling", func(t *testing.T) {
		t.Parallel()
		path := writeSource(t, "compact.rb", `class Admin::Base < ActionController::Base
end
`)
		payload, err := Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}
		classItem := assertBucketName(t, payload, "classes", "Base")
		if got := classItem["qualified_name"]; got != "Admin::Base" {
			t.Fatalf("classes[Base][qualified_name] = %#v, want Admin::Base", got)
		}
	})
}

// TestParseEmitsQualifiedBasesPreservesAbsoluteMarker pins the #5733 P1 fix
// (codex review of #5500): a superclass declared with an explicit absolute
// constant path (`class Admin::OrdersController < ::Base`) is real Ruby's
// signal to resolve the base starting at Object with NO enclosing-namespace
// search — it is semantically different from the bare relative "Base", which
// Ruby resolves via Module.nesting. Before this fix, superclassQualifiedName
// stripped the leading "::" when building qualified_bases, so the absolute
// reference was indistinguishable from a relative one once persisted. The
// #5500 lexical-scope-aware candidate restriction (rubycontroller.onwardHop)
// then wrongly searched the enclosing namespace for an absolute ref, letting
// an unrelated in-corpus class sharing the same last segment and namespace
// (e.g. "Admin::Base") masquerade as the true (possibly external/gem)
// top-level "::Base" referent. qualified_bases must carry the leading "::" so
// the reducer's repo-wide registry — and, through it, rubycontroller — can
// keep it out of the enclosing-namespace search.
func TestParseEmitsQualifiedBasesPreservesAbsoluteMarker(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "admin_orders_controller.rb", `module Admin
  class OrdersController < ::Base
    def index
      true
    end
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	classItem := assertBucketName(t, payload, "classes", "OrdersController")
	if got := classItem["bases"]; !reflect.DeepEqual(got, []string{"Base"}) {
		t.Fatalf("classes[OrdersController][bases] = %#v, want [Base] (last-segment, unaffected)", got)
	}
	if got := classItem["qualified_bases"]; !reflect.DeepEqual(got, []string{"::Base"}) {
		t.Fatalf("classes[OrdersController][qualified_bases] = %#v, want [::Base] (absolute marker preserved)", got)
	}
}

// TestParseRootsControllerActionThroughNamespacedSameFileBase is the #5376 P1
// parse-time regression: a genuine controller whose base is a namespaced class
// defined in the SAME file (module Admin; class Base < ActionController::Base)
// must still root its actions. The old same-file walk keyed the registry by the
// simple name "Base" but the base reference "Admin::Base" kept its qualifier, so
// IsKnownClass failed and the action was silently NOT rooted — a parse-time
// false positive the F1 floor now rescues (qualified unresolved base keeps).
func TestParseRootsControllerActionThroughNamespacedSameFileBase(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "namespaced_controller.rb", `module Admin
  class Base < ActionController::Base
  end
end

class OrdersController < Admin::Base
  def index
    true
  end
end
`)
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	fn := assertBucketName(t, payload, "functions", "index")
	kinds, _ := fn["dead_code_root_kinds"].([]string)
	found := false
	for _, k := range kinds {
		if k == "ruby.rails_controller_action" {
			found = true
		}
	}
	if !found {
		t.Fatalf("OrdersController#index must root as ruby.rails_controller_action through a namespaced same-file base, got %#v", kinds)
	}
}

// TestParseOmitsQualifiedBasesForSuperclasslessClass proves qualified_bases is
// additive and absent when a class declares no superclass, so the fact stays a
// minor, optional payload field.
func TestParseOmitsQualifiedBasesForSuperclasslessClass(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "poro.rb", `class OrderService
  def call
    true
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	classItem := assertBucketName(t, payload, "classes", "OrderService")
	if _, ok := classItem["qualified_bases"]; ok {
		t.Fatalf("classes[OrderService] must not carry qualified_bases, got %#v", classItem["qualified_bases"])
	}
	if _, ok := classItem["bases"]; ok {
		t.Fatalf("classes[OrderService] must not carry bases, got %#v", classItem["bases"])
	}
}

func TestParseCapturesConstantsAndKeepsContextAcrossNestedBlocks(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "controller.rb", `class OrdersController
  DEFAULT_LIMIT = 25

  def self.call(env)
    Rails.application.routes.draw do
      get "/orders", to: "orders#index"
    end

    if env.ready?
      limit = DEFAULT_LIMIT
    end

    @last_limit = limit
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	constant := assertBucketName(t, payload, "variables", "DEFAULT_LIMIT")
	if got := constant["context"]; got != "OrdersController" {
		t.Fatalf("variables[DEFAULT_LIMIT][context] = %#v, want OrdersController", got)
	}
	if got := constant["context_type"]; got != "class" {
		t.Fatalf("variables[DEFAULT_LIMIT][context_type] = %#v, want class", got)
	}

	function := assertBucketName(t, payload, "functions", "call")
	if got := function["type"]; got != "singleton" {
		t.Fatalf("functions[call][type] = %#v, want singleton", got)
	}
	if got := function["class_context"]; got != "OrdersController" {
		t.Fatalf("functions[call][class_context] = %#v, want OrdersController", got)
	}

	lastLimit := assertBucketName(t, payload, "variables", "@last_limit")
	if got := lastLimit["context"]; got != "call" {
		t.Fatalf("variables[@last_limit][context] = %#v, want call", got)
	}
	if got := lastLimit["context_type"]; got != "def" {
		t.Fatalf("variables[@last_limit][context_type] = %#v, want def", got)
	}
	assertBucketField(t, payload, "function_calls", "full_name", "Rails.application.routes.draw")
	assertBucketField(t, payload, "function_calls", "full_name", "env.ready?")
}

func TestParseRubySingletonClassExtraction(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "singleton.rb", `class Builder
  class << self
    def from_block(name)
      new(name)
    end
  end
end
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	fn := assertBucketName(t, payload, "functions", "from_block")
	if got := fn["type"]; got != "singleton" {
		t.Fatalf("functions[from_block][type] = %#v, want singleton", got)
	}
	if got := fn["class_context"]; got != "Builder" {
		t.Fatalf("functions[from_block][class_context] = %#v, want Builder", got)
	}
}

func TestParseRubyVisibilityTransitions(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "visibility.rb", `class Worker

  def public_method
    helper
  end

  private

  def private_method
    helper
  end

  protected

  def protected_method
    helper
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "functions", "public_method")
	assertBucketName(t, payload, "functions", "private_method")
	assertBucketName(t, payload, "functions", "protected_method")
}

func TestParseRubyModuleExtraction(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "modules.rb", `module Namespace
  module InnerModule
    class Worker
      def perform
        "work"
      end
    end
  end
end
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "modules", "Namespace")
	assertBucketName(t, payload, "modules", "InnerModule")
	assertBucketName(t, payload, "classes", "Worker")

	fn := assertBucketName(t, payload, "functions", "perform")
	if got := fn["class_context"]; got != "Worker" {
		t.Fatalf("functions[perform][class_context] = %#v, want Worker", got)
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
