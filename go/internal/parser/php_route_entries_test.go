// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPEmitsSymfonyRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "ReportController.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php
namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

final class ReportController {
    #[Route('/reports/{id}', methods: ['GET'], name: 'reports_show')]
    public function show(): string {
        return 'show';
    }

    #[Route(path: '/reports', methods: ['POST'])]
    public function create(): string {
        return 'create';
    }

    #[Route('/reports/{id}/preview')]
    public function preview(): string {
        return 'preview';
    }

    #[Route($dynamicPath, methods: ['DELETE'])]
    public function dynamicPath(): string {
        return 'dynamic';
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "symfony")
	assertNestedRouteEntriesEqual(t, got, "symfony", []map[string]string{
		{"method": "GET", "path": "/reports/{id}", "handler": "ReportController.show"},
		{"method": "POST", "path": "/reports", "handler": "ReportController.create"},
		{"method": "ANY", "path": "/reports/{id}/preview", "handler": "ReportController.preview"},
	})
}

func TestDefaultEngineParsePathPHPSkipsNonExactSymfonyRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "DynamicController.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php
namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

final class DynamicController {
    #[Route(self::DYNAMIC_PATH, methods: ['GET'])]
    public function dynamicPath(): string {
        return 'dynamic';
    }

    #[Route('/dynamic-method', methods: [self::METHOD])]
    public function dynamicMethod(): string {
        return 'dynamic';
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	nested, ok := semantics["symfony"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := nested["route_entries"]; ok {
		t.Fatalf("framework_semantics.symfony.route_entries = %#v, want absent for non-exact Symfony routes", nested["route_entries"])
	}
}

func TestDefaultEngineParsePathPHPSkipsUnresolvedBareRouteAttribute(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "CustomController.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php
namespace App\Http\Controllers;

final class CustomController {
    #[Route('/custom', methods: ['GET'])]
    public function custom(): string {
        return 'custom';
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	if semantics, ok := got["framework_semantics"]; ok {
		t.Fatalf("framework_semantics = %#v, want absent for an unresolved bare Route attribute", semantics)
	}
}

func TestDefaultEngineParsePathPHPEmitsSlimRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "routes.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

use Slim\Factory\AppFactory;

$app = AppFactory::create();

$app->get('/', function ($req, $res) { return $res; });
$app->post('/users', \App\Action\CreateUserAction::class);
$app->map(['GET', 'POST'], '/multi', 'Handler:method');
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "slim")
	assertNestedRouteEntriesEqual(t, got, "slim", []map[string]string{
		{"method": "GET", "path": "/", "handler": ""},
		{"method": "POST", "path": "/users", "handler": "CreateUserAction"},
		{"method": "GET", "path": "/multi", "handler": "Handler:method"},
		{"method": "POST", "path": "/multi", "handler": "Handler:method"},
	})
}

func TestDefaultEngineParsePathPHPSkipsNonSlimGetCall(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "not_slim.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

$collection = new SomeCollection();
$item = $collection->get($id);
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := semantics["slim"]; ok {
		t.Fatalf("framework_semantics.slim should be absent for non-Slim get() call")
	}
}

func TestDefaultEngineParsePathPHPEmitsSlimRoutesFromSkeleton(t *testing.T) {
	// Parses the real slimphp/Slim-Skeleton app/routes.php directly.
	// The file must exist on disk; skip if not found (e.g. in CI).
	skeletonPath := "/tmp/Slim-Skeleton/app/routes.php"
	if _, err := filepath.Abs(skeletonPath); err != nil {
		t.Skipf("Slim-Skeleton not available at %s: %v", skeletonPath, err)
	}

	repoRoot := "/tmp/Slim-Skeleton"

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, skeletonPath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", skeletonPath, err)
	}

	assertFrameworksEqual(t, got, "slim")
	slim, ok := got["framework_semantics"].(map[string]any)["slim"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics.slim not found")
	}
	entries, ok := slim["route_entries"].([]map[string]string)
	if !ok {
		t.Fatalf("slim.route_entries not found or wrong type")
	}
	if len(entries) == 0 {
		t.Fatalf("slim.route_entries is empty, want Slim routes detected")
	}
	t.Logf("Slim-Skeleton route entries detected: %d", len(entries))
	for _, e := range entries {
		t.Logf("  %s %s -> %s", e["method"], e["path"], e["handler"])
	}
	// Assert key routes are present.
	wantRoutes := []map[string]string{
		{"method": "OPTIONS", "path": "/{routes:.*}", "handler": ""},
		{"method": "GET", "path": "/", "handler": ""},
	}
	for _, want := range wantRoutes {
		found := false
		for _, e := range entries {
			if e["method"] == want["method"] && e["path"] == want["path"] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected Slim route %s %s not found in entries: %#v", want["method"], want["path"], entries)
		}
	}
}
