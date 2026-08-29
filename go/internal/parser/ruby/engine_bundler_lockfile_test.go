// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathRubyBundlerLockfileGitHubSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Gemfile.lock")
	writeTestFile(
		t,
		filePath,
		`GIT
  remote: https://github.com/rails/rails.git
  revision: abc123def456
  specs:
    rails (8.0.0)

PATH
  remote: ../vendor/gems/auth
  specs:
    auth (1.2.3)

GEM
  remote: https://rubygems.org/
  specs:
    rack (3.1.0)

DEPENDENCIES
  rails!
  auth!
  rack
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	rails := parsertest.AssertBucketItemByName(t, got, "variables", "rails")
	assertStringFieldValue(t, rails, "value", "8.0.0")
	assertStringFieldValue(t, rails, "source_type", "git")
	assertStringFieldValue(t, rails, "source_path", "https://github.com/rails/rails.git")

	auth := parsertest.AssertBucketItemByName(t, got, "variables", "auth")
	assertStringFieldValue(t, auth, "value", "1.2.3")
	assertStringFieldValue(t, auth, "source_type", "path")
	assertStringFieldValue(t, auth, "source_path", "../vendor/gems/auth")

	rack := parsertest.AssertBucketItemByName(t, got, "variables", "rack")
	assertStringFieldValue(t, rack, "value", "3.1.0")
	assertStringFieldValue(t, rack, "source_type", "rubygems")
}
