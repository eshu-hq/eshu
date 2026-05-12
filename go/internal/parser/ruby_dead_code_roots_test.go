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
