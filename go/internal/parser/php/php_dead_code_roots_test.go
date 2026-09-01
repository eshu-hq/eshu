// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "routes.php")
	parsertest.WriteFile(
		t,
		sourcePath,
		`<?php
namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

function main(): void {
    wordpress_callback();
}

function wordpress_callback(): void {
}

function unused_php_helper(): string {
    return 'unused';
}

add_action('init', 'wordpress_callback');

interface Reportable {
    public function render(string $format): string;
}

trait Auditable {
    public function bootAuditable(): void {
    }
}

final class ReportController implements Reportable {
    use Auditable;

    public function __construct() {
    }

    public function render(string $format): string {
        return $format;
    }

    #[Route('/reports/{id}', name: 'reports_show')]
    public function show(): string {
        return 'show';
    }

    public function __invoke(): string {
        return 'invoke';
    }

    public function supportOnly(): string {
        return 'support';
    }

    private function helper(): string {
        return 'helper';
    }
}

Route::get('/reports/{id}', [ReportController::class, 'show']);
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

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "main"), "dead_code_root_kinds", "php.script_entrypoint")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "wordpress_callback"), "dead_code_root_kinds", "php.wordpress_hook_callback")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "render", "Reportable"), "dead_code_root_kinds", "php.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "bootAuditable", "Auditable"), "dead_code_root_kinds", "php.trait_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "__construct", "ReportController"), "dead_code_root_kinds", "php.constructor")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "__construct", "ReportController"), "dead_code_root_kinds", "php.magic_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "render", "ReportController"), "dead_code_root_kinds", "php.interface_implementation_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "show", "ReportController"), "dead_code_root_kinds", "php.framework_controller_action")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "show", "ReportController"), "dead_code_root_kinds", "php.route_handler")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "show", "ReportController"), "dead_code_root_kinds", "php.symfony_route_attribute")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "__invoke", "ReportController"), "dead_code_root_kinds", "php.magic_method")

	for _, tc := range []struct {
		name         string
		classContext string
	}{
		{name: "unused_php_helper"},
		{name: "supportOnly", classContext: "ReportController"},
		{name: "helper", classContext: "ReportController"},
	} {
		function := parsertest.AssertBucketItemByName(t, got, "functions", tc.name)
		if tc.classContext != "" {
			function = parsertest.AssertFunctionByNameAndClass(t, got, tc.name, tc.classContext)
		}
		if function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s.%s dead_code_root_kinds = %#v, want nil", tc.classContext, tc.name, function["dead_code_root_kinds"])
		}
	}
}

func TestDefaultEngineParsePathPHPKeepsRootKindsForNextLineTypeBraces(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "psr.php")
	parsertest.WriteFile(
		t,
		sourcePath,
		`<?php
interface Runnable
{
    public function run(): void;
}

trait Boots
{
    public function bootBoots(): void
    {
    }
}

class WorkerController implements Runnable
{
    use Boots;

    public function __construct()
    {
    }

    public function run(): void
    {
    }

    public function supportOnly(): void
    {
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

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "run", "Runnable"), "dead_code_root_kinds", "php.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "bootBoots", "Boots"), "dead_code_root_kinds", "php.trait_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "__construct", "WorkerController"), "dead_code_root_kinds", "php.constructor")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "__construct", "WorkerController"), "dead_code_root_kinds", "php.magic_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "run", "WorkerController"), "dead_code_root_kinds", "php.interface_implementation_method")
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "supportOnly", "WorkerController"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("WorkerController.supportOnly dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathPHPDoesNotRootAmbiguousSyntax(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "ambiguous.php")
	parsertest.WriteFile(
		t,
		sourcePath,
		`<?php
function commentedHook(): void {
}

// add_action('init', 'commentedHook');
/* Route::get('/support', [SupportController::class, 'commentedRoute']); */

class SupportController {
    public function unusedAction(): void {
    }

    public function commentedRoute(): void {
    }

    #[MyRoute('/support')]
    public function myRouteOnly(): void {
    }

    public function __legacyHelper(): void {
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

	for _, tc := range []struct {
		name         string
		classContext string
	}{
		{name: "commentedHook"},
		{name: "unusedAction", classContext: "SupportController"},
		{name: "commentedRoute", classContext: "SupportController"},
		{name: "myRouteOnly", classContext: "SupportController"},
		{name: "__legacyHelper", classContext: "SupportController"},
	} {
		function := parsertest.AssertBucketItemByName(t, got, "functions", tc.name)
		if tc.classContext != "" {
			function = parsertest.AssertFunctionByNameAndClass(t, got, tc.name, tc.classContext)
		}
		if function["dead_code_root_kinds"] != nil {
			t.Fatalf("%s.%s dead_code_root_kinds = %#v, want nil", tc.classContext, tc.name, function["dead_code_root_kinds"])
		}
	}
}

func TestDefaultEngineParsePathPHPRootsInheritedInterfaceMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "interfaces.php")
	parsertest.WriteFile(
		t,
		sourcePath,
		`<?php
interface ParentContract {
    public function inherited(array $options = ['a', 'b']): void;
}

interface ChildContract extends ParentContract {
    public function child(): void;
}

class ChildImplementation implements ChildContract {
    public function inherited(array $options = ['a', 'b']): void {
    }

    public function child(): void {
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

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "inherited", "ParentContract"), "dead_code_root_kinds", "php.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "child", "ChildContract"), "dead_code_root_kinds", "php.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "inherited", "ChildImplementation"), "dead_code_root_kinds", "php.interface_implementation_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "child", "ChildImplementation"), "dead_code_root_kinds", "php.interface_implementation_method")
}

func TestDefaultEngineParsePathPHPDeadCodeFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	repoRoot := phpFixturePath("deadcode", "php")
	sourcePath := phpFixturePath("deadcode", "php", "app.php")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "main"), "dead_code_root_kinds", "php.script_entrypoint")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "selectedPhpHandler"), "dead_code_root_kinds", "php.wordpress_hook_callback")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "indexAction", "PublicPhpController"), "dead_code_root_kinds", "php.framework_controller_action")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "indexAction", "PublicPhpController"), "dead_code_root_kinds", "php.route_handler")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "render", "PhpRenderable"), "dead_code_root_kinds", "php.interface_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "render", "PublicPhpController"), "dead_code_root_kinds", "php.interface_implementation_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "bootPublicPhpController", "PublicPhpTrait"), "dead_code_root_kinds", "php.trait_method")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "__invoke", "PublicPhpController"), "dead_code_root_kinds", "php.magic_method")
	if unused := parsertest.AssertBucketItemByName(t, got, "functions", "unusedPhpHelper"); unused["dead_code_root_kinds"] != nil {
		t.Fatalf("unusedPhpHelper dead_code_root_kinds = %#v, want nil", unused["dead_code_root_kinds"])
	}
	if helper := parsertest.AssertFunctionByNameAndClass(t, got, "helper", "PublicPhpController"); helper["dead_code_root_kinds"] != nil {
		t.Fatalf("PublicPhpController.helper dead_code_root_kinds = %#v, want nil", helper["dead_code_root_kinds"])
	}
}
