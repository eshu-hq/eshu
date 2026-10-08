// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPZF1ControllerActionsAreRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "controllers.php")
	parsertest.WriteFile(
		t,
		sourcePath,
		`<?php
class UserController extends Zend_Controller_Action {
    public function indexAction(): void {
    }

    public function showAction(): void {
    }

    public function helperMethod(): void {
    }

    private function secretAction(): void {
    }

    public function Action(): void {
    }
}

class AdminController extends \Zend_Controller_Action {
    public function listAction(): void {
    }
}

class PlainController {
    public function indexAction(): void {
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "indexAction", "UserController"), "dead_code_root_kinds", "php.zf1_controller_action")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "showAction", "UserController"), "dead_code_root_kinds", "php.zf1_controller_action")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "listAction", "AdminController"), "dead_code_root_kinds", "php.zf1_controller_action")

	for _, tc := range []struct {
		name         string
		classContext string
	}{
		{name: "helperMethod", classContext: "UserController"},
		{name: "secretAction", classContext: "UserController"},
		{name: "Action", classContext: "UserController"},
		{name: "indexAction", classContext: "PlainController"},
	} {
		function := parsertest.AssertFunctionByNameAndClass(t, got, tc.name, tc.classContext)
		if function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s.%s dead_code_root_kinds = %#v, want nil", tc.classContext, tc.name, function["dead_code_root_kinds"])
		}
	}
}
