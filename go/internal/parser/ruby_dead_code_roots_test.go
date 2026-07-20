// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathRubyEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "app", "controllers", "users_controller.rb")
	writeTestFile(
		t,
		filePath,
		`class Admin::UsersController
  before_action :authenticate_user!

  def index
    direct_helper
  end

  private

  def authenticate_user!
    true
  end

  def internal_helper
    true
  end
end

class DynamicEndpoint
  def method_missing(name, *args)
    public_send(name, *args)
  end

  def respond_to_missing?(name, include_private = false)
    true
  end
end

def main
  direct_helper
end

def direct_helper
  true
end

def reflective_dispatch
  method(:direct_helper).call
end

if __FILE__ == $PROGRAM_NAME
  main
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", filePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "index"), "dead_code_root_kinds", "ruby.rails_controller_action")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "authenticate_user!"), "dead_code_root_kinds", "ruby.rails_callback_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "method_missing"), "dead_code_root_kinds", "ruby.dynamic_dispatch_hook")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "respond_to_missing?"), "dead_code_root_kinds", "ruby.dynamic_dispatch_hook")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "ruby.script_entrypoint")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "direct_helper"), "dead_code_root_kinds", "ruby.method_reference_target")
	if helper := assertFunctionByName(t, got, "internal_helper"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("internal_helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathRubyDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "ruby")
	sourcePath := repoFixturePath("deadcode", "ruby", "app.rb")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "index"), "dead_code_root_kinds", "ruby.rails_controller_action")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "authenticate_ruby_user!"), "dead_code_root_kinds", "ruby.rails_callback_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "method_missing"), "dead_code_root_kinds", "ruby.dynamic_dispatch_hook")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "main"), "dead_code_root_kinds", "ruby.script_entrypoint")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "direct_ruby_helper"), "dead_code_root_kinds", "ruby.method_reference_target")
	if helper := assertFunctionByName(t, got, "unused_ruby_helper"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("unused_ruby_helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathRubyEmitsReceiverlessHelperCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "app", "controllers", "api_controller.rb")
	writeTestFile(
		t,
		filePath,
		`class Admin::ApiController
  def create
    api_key.scopes = build_scopes
    log_api_key(api_key, changes: api_key.saved_changes)
  end

  def undo_revoke_key
    log_api_key_restore api_key
  end

  private

  def build_scopes
    build_params(params)
  end

  def build_params(params)
    params
  end

  def log_api_key(*args)
    StaffActionLogger.new.log_api_key(*args)
  end

  def log_api_key_restore(*args)
    StaffActionLogger.new.log_api_key_restore(*args)
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
		t.Fatalf("ParsePath(%s) error = %v, want nil", filePath, err)
	}

	create := assertFunctionByName(t, got, "create")
	if got, want := create["end_line"], 5; got != want {
		t.Fatalf("create end_line = %#v, want %#v", got, want)
	}
	for _, callName := range []string{"build_scopes", "log_api_key", "log_api_key_restore", "build_params"} {
		call := assertBucketItemByName(t, got, "function_calls", callName)
		if got := call["class_context"]; got != "ApiController" {
			t.Fatalf("function_calls[%s][class_context] = %#v, want ApiController; call=%#v", callName, got, call)
		}
	}
}

func TestDefaultEngineParsePathRubyRootsArrayCallbackMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "app", "controllers", "accounts_controller.rb")
	writeTestFile(
		t,
		filePath,
		`class Admin::AccountsController
  before_action [:authenticate_user!, :set_account]

  def authenticate_user!
    true
  end

  def set_account
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
		t.Fatalf("ParsePath(%s) error = %v, want nil", filePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "authenticate_user!"), "dead_code_root_kinds", "ruby.rails_callback_method")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "set_account"), "dead_code_root_kinds", "ruby.rails_callback_method")
}

// TestDefaultEngineParsePathRubyGatesControllerActionOnSuperclassChain
// characterizes issue #5337 Detector 1 as amended by the #5376 P1 safety floor:
// rubyIsRailsController gates on a same-file transitive superclass walk, not a
// "*Controller" name suffix. A class whose superclass chain resolves to an
// unresolved SIMPLE non-controller name (< Thor) must NOT root its actions. But
// a QUALIFIED base the file cannot resolve (< Sinatra::Base) must KEEP rooting
// under the #5376 F1 floor: a namespaced base could be a controller base in a
// gem or namespace this file never sees, so it must never be treated as a
// positive non-controller downgrade (the old behavior flagged a genuine
// `OrdersController < Admin::Base` dead). A class with no declared superclass
// keeps rooting (deliberate residual: per-file parsing cannot distinguish a
// reopened Rails controller from a bare PORO). A transitive chain through a
// same-file intermediate class, and an unresolved "*Controller"-suffixed chain
// that leaves the file, must both still root.
func TestDefaultEngineParsePathRubyGatesControllerActionOnSuperclassChain(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "gating.rb")
	writeTestFile(
		t,
		filePath,
		`class ReportController < Thor
  def run_report
    true
  end
end

class LegacyApp < Sinatra::Base
  def route_handler
    true
  end
end

class WidgetsController
  def show_widget
    true
  end
end

class OrdersController < ApplicationController
  def list_orders
    true
  end
end

class BaseControllerLocal < ActionController::Base
end

class UsersController < BaseControllerLocal
  def list_users
    true
  end
end

class ApiController < Api::BaseController
  def list_api
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
		t.Fatalf("ParsePath(%s) error = %v, want nil", filePath, err)
	}

	// Reject: declared superclass is an unresolved SIMPLE non-controller name.
	assertParserStringSliceNotContains(t, assertFunctionByName(t, got, "run_report"), "dead_code_root_kinds", "ruby.rails_controller_action")

	// Keep (#5376 F1): an unresolved QUALIFIED base (< Sinatra::Base) is
	// keep-biased — it could be a controller base defined elsewhere, so it must
	// never be treated as a positive non-controller downgrade.
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "route_handler"), "dead_code_root_kinds", "ruby.rails_controller_action")

	// Keep: no declared superclass at all.
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "show_widget"), "dead_code_root_kinds", "ruby.rails_controller_action")

	// Accept: exact accepted base.
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "list_orders"), "dead_code_root_kinds", "ruby.rails_controller_action")

	// Accept: transitive chain through a same-file intermediate class.
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "list_users"), "dead_code_root_kinds", "ruby.rails_controller_action")

	// Accept: unresolved superclass name ending in "Controller" (chain leaves the file).
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "list_api"), "dead_code_root_kinds", "ruby.rails_controller_action")
}

// TestDefaultEngineParsePathRubySameFileShortNameCollisionResolvesToLastRegistered
// pins the documented same-file short-name-collision limitation of
// rubyClassRegistry (issue #5337 P2-1): two classes in one file whose simple
// names collide across namespaces ("Admin::BaseController" and
// "Api::BaseController", both keyed as "BaseController") share one registry
// entry, so the last one registered in source order wins. Here Api::BaseController
// (< Thor) is declared after Admin::BaseController (< ActionController::Base), so
// a third class extending the bare "BaseController" resolves against < Thor and
// does NOT root — the collision-affected verdict. This asserts the current
// deterministic behavior (source-order last-wins), not the ideal one; correct
// namespace-aware, repo-wide resolution is the reducer follow-up #5376.
func TestDefaultEngineParsePathRubySameFileShortNameCollisionResolvesToLastRegistered(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "collision.rb")
	writeTestFile(
		t,
		filePath,
		`class Admin::BaseController < ActionController::Base
end

class Api::BaseController < Thor
  def api_action
    true
  end
end

class UsersController < BaseController
  def list_users
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
		t.Fatalf("ParsePath(%s) error = %v, want nil", filePath, err)
	}

	// The "BaseController" registry key was last written by Api::BaseController
	// (< Thor), so UsersController's chain resolves to Thor and the root is
	// dropped. Documented collision limitation; #5376 resolves it properly.
	assertParserStringSliceNotContains(t, assertFunctionByName(t, got, "list_users"), "dead_code_root_kinds", "ruby.rails_controller_action")
}

func TestDefaultEngineParsePathRubyRejectsNonEqualityScriptGuard(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "negative_guard.rb")
	writeTestFile(
		t,
		filePath,
		`def positive_entrypoint
  true
end

def negative_only
  true
end

if __FILE__ == $0
  positive_entrypoint
end

if __FILE__ != $0
  negative_only
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", filePath, err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "positive_entrypoint"), "dead_code_root_kinds", "ruby.script_entrypoint")
	assertParserStringSliceNotContains(t, assertFunctionByName(t, got, "negative_only"), "dead_code_root_kinds", "ruby.script_entrypoint")
}
