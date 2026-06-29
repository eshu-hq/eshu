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
