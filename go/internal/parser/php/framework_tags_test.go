// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPTagsSymfonyAttributedMethod(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "ReportController.php")
	parsertest.WriteFile(
		t,
		sourcePath,
		`<?php
namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

final class ReportController {
    #[Route('/reports/{id}', methods: ['GET'])]
    public function show(): string {
        return 'show';
    }

    public function helper(): string {
        return 'helper';
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

	parsertest.AssertStringFieldValue(t, parsertest.AssertFunctionByNameAndClass(t, got, "show", "ReportController"), "framework", "symfony")
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "helper", "ReportController"); helper["framework"] != nil {
		t.Fatalf("ReportController.helper framework = %#v, want nil", helper["framework"])
	}
}

func TestDefaultEngineParsePathPHPTagsWordPressHookCallback(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "plugin.php")
	parsertest.WriteFile(
		t,
		sourcePath,
		`<?php
function wordpress_init_callback(): void {
}

function plain_helper(): void {
}

add_action('init', 'wordpress_init_callback');
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

	parsertest.AssertStringFieldValue(t, parsertest.AssertBucketItemByName(t, got, "functions", "wordpress_init_callback"), "framework", "wordpress")
	if helper := parsertest.AssertBucketItemByName(t, got, "functions", "plain_helper"); helper["framework"] != nil {
		t.Fatalf("plain_helper framework = %#v, want nil", helper["framework"])
	}
}

func TestDefaultEngineParsePathPHPTagsZF1ControllerAction(t *testing.T) {
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

    public function helperMethod(): void {
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

	parsertest.AssertStringFieldValue(t, parsertest.AssertFunctionByNameAndClass(t, got, "indexAction", "UserController"), "framework", "zend_framework_1")
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "helperMethod", "UserController"); helper["framework"] != nil {
		t.Fatalf("UserController.helperMethod framework = %#v, want nil", helper["framework"])
	}
}

func TestDefaultEngineParsePathPHPSkipsFrameworkTagOnMultipleEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "MixedController.php")
	parsertest.WriteFile(
		t,
		sourcePath,
		`<?php
use Symfony\Component\Routing\Attribute\Route;

class MixedController extends Zend_Controller_Action {
    #[Route('/mixed', methods: ['GET'])]
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

	// Two frameworks claim the same method (symfony attribute + ZF1 base):
	// ambiguous stays silent rather than guessing.
	if method := parsertest.AssertFunctionByNameAndClass(t, got, "indexAction", "MixedController"); method["framework"] != nil {
		t.Fatalf("MixedController.indexAction framework = %#v, want nil", method["framework"])
	}
}
