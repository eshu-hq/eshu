// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestDefaultEngineParsePathPHPEndLineSpansDeclarations pins that PHP
// declarations record their full span: end_line is the declaration's last
// line, not a copy of line_number (#7641). The reducer uses the span to find
// the function containing a call, so a single-line span drops every in-function
// call's caller.
func TestDefaultEngineParsePathPHPEndLineSpansDeclarations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "end_line.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
function topLevel() {
    $x = 1;
    return $x;
}

class Service {
    public function run() {
        return 1;
    }
}

interface HasRun {
    public function run();
}

trait Helper {
    public function help() {
        return 2;
    }
}

$obj = new class {
    public function init() {
        return 3;
    }
};
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

	topLevel := parsertest.AssertFunctionByNameAndClass(t, got, "topLevel", "")
	parsertest.AssertIntFieldValue(t, topLevel, "line_number", 2)
	parsertest.AssertIntFieldValue(t, topLevel, "end_line", 5)

	service := parsertest.AssertBucketItemByName(t, got, "classes", "Service")
	parsertest.AssertIntFieldValue(t, service, "line_number", 7)
	parsertest.AssertIntFieldValue(t, service, "end_line", 11)

	run := parsertest.AssertFunctionByNameAndClass(t, got, "run", "Service")
	parsertest.AssertIntFieldValue(t, run, "line_number", 8)
	parsertest.AssertIntFieldValue(t, run, "end_line", 10)

	iface := parsertest.AssertBucketItemByName(t, got, "interfaces", "HasRun")
	parsertest.AssertIntFieldValue(t, iface, "line_number", 13)
	parsertest.AssertIntFieldValue(t, iface, "end_line", 15)

	helper := parsertest.AssertBucketItemByName(t, got, "traits", "Helper")
	parsertest.AssertIntFieldValue(t, helper, "line_number", 17)
	parsertest.AssertIntFieldValue(t, helper, "end_line", 21)

	help := parsertest.AssertFunctionByNameAndClass(t, got, "help", "Helper")
	parsertest.AssertIntFieldValue(t, help, "line_number", 18)
	parsertest.AssertIntFieldValue(t, help, "end_line", 20)

	anon := parsertest.AssertBucketItemByName(t, got, "classes", "anonymous_class_23")
	parsertest.AssertIntFieldValue(t, anon, "line_number", 23)
	parsertest.AssertIntFieldValue(t, anon, "end_line", 27)
}
