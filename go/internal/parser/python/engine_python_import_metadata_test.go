// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPythonEmitsImportSourceAndAliasMetadata(
	t *testing.T,
) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imports.py")
	writeTestFile(
		t,
		filePath,
		`from lib.factory import create_app as make_app, helper
import pkg.mod as mod
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

	createApp := parsertest.AssertBucketItemByName(t, got, "imports", "create_app")
	parsertest.AssertStringFieldValue(t, createApp, "alias", "make_app")
	parsertest.AssertStringFieldValue(t, createApp, "source", "lib.factory")

	helper := parsertest.AssertBucketItemByName(t, got, "imports", "helper")
	parsertest.AssertStringFieldValue(t, helper, "source", "lib.factory")

	moduleAlias := parsertest.AssertBucketItemByName(t, got, "imports", "pkg.mod")
	parsertest.AssertStringFieldValue(t, moduleAlias, "alias", "mod")
	parsertest.AssertStringFieldValue(t, moduleAlias, "source", "pkg.mod")
}
