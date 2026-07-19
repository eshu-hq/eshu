// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestPackageJSONEmitsRealLineNumbers pins issue #5329: package.json
// dependency and script rows must carry the real source line of their JSON
// key, not a synthetic 1,2,3... per-section counter. Before the fix, "zod"
// (line 9) and "@types/node" (line 10) both emitted from a counter starting
// at 1 for the "dependencies" section, so this test failed RED against the
// old lineNumber:=1; lineNumber++ code with line_number=1 and 2 instead of
// the real 9 and 10.
func TestPackageJSONEmitsRealLineNumbers(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "package.json", `{
  "zeta": true,
  "alpha": true,
  "scripts": {
    "test": "vitest",
    "build": "tsc"
  },
  "dependencies": {
    "zod": "^3.0.0",
    "@types/node": "^20.0.0"
  },
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	variables, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	byName := dependencyRowsByName(variables)

	wantLines := map[string]int{
		"zod":         9,
		"@types/node": 10,
		"typescript":  13,
	}
	for name, want := range wantLines {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("dependency row %q missing", name)
		}
		got, ok := row["line_number"].(int)
		if !ok {
			t.Fatalf("row[%q][line_number] = %T, want int", name, row["line_number"])
		}
		if got != want {
			t.Errorf("row[%q][line_number] = %d, want %d", name, got, want)
		}
	}

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	functionsByName := make(map[string]map[string]any, len(functions))
	for _, row := range functions {
		name, _ := row["name"].(string)
		functionsByName[name] = row
	}

	wantScriptLines := map[string]int{"test": 5, "build": 6}
	for name, want := range wantScriptLines {
		row, ok := functionsByName[name]
		if !ok {
			t.Fatalf("script row %q missing", name)
		}
		got, ok := row["line_number"].(int)
		if !ok {
			t.Fatalf("row[%q][line_number] = %T, want int", name, row["line_number"])
		}
		if got != want {
			t.Errorf("row[%q][line_number] = %d, want %d", name, got, want)
		}
		if endLine, ok := row["end_line"].(int); !ok || endLine != want {
			t.Errorf("row[%q][end_line] = %v, want %d", name, row["end_line"], want)
		}
	}

	// Distinct real lines prove this is not a counter: zod (9) < @types/node
	// (10) matches both source order and a counter's output, but the
	// dependencies section starting at line 8 (not line 1) is the signal a
	// counter could never produce.
	if byName["zod"]["line_number"].(int) == 1 {
		t.Fatalf("zod line_number = 1, looks like the old fabricated counter start, want real line 9")
	}
}

// TestTSConfigEmitsRealLineNumbersPerSection pins issue #5329 for
// tsconfig.json: extends, each references[] element, and each
// compilerOptions.paths alias must each carry their own real source line,
// not a single counter shared across all three sections (the old code
// incremented one lineNumber across extends/references/paths in row-emission
// order, unrelated to where each key actually sits in the file).
func TestTSConfigEmitsRealLineNumbersPerSection(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "tsconfig.json", `{
  "extends": "./base.json",
  "references": [
    { "path": "../core" },
    { "path": "../utils" }
  ],
  "compilerOptions": {
    "paths": {
      "@app/*": ["src/app/*"],
      "@lib/*": ["src/lib/*"]
    }
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	byName := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		byName[name] = row
	}

	wantLines := map[string]int{
		"extends":            2,
		"reference:../core":  4,
		"reference:../utils": 5,
		"path:@app/*":        9,
		"path:@lib/*":        10,
	}
	for name, want := range wantLines {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("row %q missing", name)
		}
		got, ok := row["line_number"].(int)
		if !ok {
			t.Fatalf("row[%q][line_number] = %T, want int", name, row["line_number"])
		}
		if got != want {
			t.Errorf("row[%q][line_number] = %d, want %d", name, got, want)
		}
	}
}

// TestComposerJSONEmitsRealLineNumbers pins issue #5329 for composer.json
// require/require-dev rows.
func TestComposerJSONEmitsRealLineNumbers(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "composer.json", `{
  "require": {
    "php": "^8.1",
    "symfony/console": "^6.0"
  },
  "require-dev": {
    "phpunit/phpunit": "^10.0"
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	byName := dependencyRowsByName(rows)

	wantLines := map[string]int{
		"php":             3,
		"symfony/console": 4,
		"phpunit/phpunit": 7,
	}
	for name, want := range wantLines {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("dependency row %q missing", name)
		}
		got, ok := row["line_number"].(int)
		if !ok {
			t.Fatalf("row[%q][line_number] = %T, want int", name, row["line_number"])
		}
		if got != want {
			t.Errorf("row[%q][line_number] = %d, want %d", name, got, want)
		}
	}
}

// TestPackageLockJSONEmitsRealLineNumbersNotOutputOrder pins issue #5329:
// package-lock.json rows are re-sorted alphabetically by path for
// deterministic output, so a counter tied to output position would assign
// "node_modules/rollup" (sorted first) a smaller number than
// "node_modules/vite" even though rollup's real source line (13) is *after*
// vite's (10). Real per-key lines must survive that reordering.
func TestPackageLockJSONEmitsRealLineNumbersNotOutputOrder(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "package-lock.json", `{
  "name": "eshu",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "dependencies": {
        "vite": "^5.4.11"
      }
    },
    "node_modules/vite": {
      "version": "5.4.11"
    },
    "node_modules/rollup": {
      "version": "4.9.0"
    }
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	byName := dependencyRowsByName(rows)

	vite, ok := byName["vite"]
	if !ok {
		t.Fatalf("dependency row %q missing", "vite")
	}
	rollup, ok := byName["rollup"]
	if !ok {
		t.Fatalf("dependency row %q missing", "rollup")
	}

	if got, want := vite["line_number"], 10; got != want {
		t.Errorf("vite line_number = %v, want %d", got, want)
	}
	if got, want := rollup["line_number"], 13; got != want {
		t.Errorf("rollup line_number = %v, want %d", got, want)
	}
}

// TestComposerLockEmitsRealLineNumbersNotOutputOrder pins issue #5329:
// composer.lock's "packages" section is a JSON array, and rows are re-sorted
// alphabetically by name. The array holds psr/log before monolog/monolog, so
// a counter over sorted output order would give monolog line_number=1 and
// psr/log line_number=2; the real lines are the reverse (monolog is later in
// the array, at line 7; psr/log is first, at line 3).
func TestComposerLockEmitsRealLineNumbersNotOutputOrder(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "composer.lock", `{
  "packages": [
    {
      "name": "psr/log",
      "version": "1.1.4"
    },
    {
      "name": "monolog/monolog",
      "version": "2.9.1"
    }
  ],
  "packages-dev": [
    {
      "name": "phpunit/phpunit",
      "version": "9.6.13"
    }
  ]
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	byName := dependencyRowsByName(rows)

	if got, want := byName["psr/log"]["line_number"], 3; got != want {
		t.Errorf("psr/log line_number = %v, want %d", got, want)
	}
	if got, want := byName["monolog/monolog"]["line_number"], 7; got != want {
		t.Errorf("monolog/monolog line_number = %v, want %d", got, want)
	}
	if got, want := byName["phpunit/phpunit"]["line_number"], 13; got != want {
		t.Errorf("phpunit/phpunit line_number = %v, want %d", got, want)
	}
}

// TestPipfileLockEmitsRealLineNumbers pins issue #5329 for Pipfile.lock.
func TestPipfileLockEmitsRealLineNumbers(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "Pipfile.lock", `{
  "_meta": {
    "hash": {}
  },
  "default": {
    "flask": {
      "version": "==2.0.0"
    },
    "requests": {
      "version": "==2.28.0"
    }
  },
  "develop": {
    "pytest": {
      "version": "==7.0.0"
    }
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	byName := dependencyRowsByName(rows)

	wantLines := map[string]int{"flask": 6, "requests": 9, "pytest": 14}
	for name, want := range wantLines {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("dependency row %q missing", name)
		}
		if got := row["line_number"]; got != want {
			t.Errorf("row[%q][line_number] = %v, want %d", name, got, want)
		}
	}
}

// TestNugetPackagesLockEmitsRealLineNumbers pins issue #5329 for
// packages.lock.json (NuGet), whose dependency rows are indexed two object
// levels deep (target framework, then package name).
func TestNugetPackagesLockEmitsRealLineNumbers(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "packages.lock.json", `{
  "version": 1,
  "dependencies": {
    "net6.0": {
      "Newtonsoft.Json": {
        "type": "Direct",
        "requested": "[13.0.1, )",
        "resolved": "13.0.1"
      },
      "Serilog": {
        "type": "Direct",
        "requested": "[2.10.0, )",
        "resolved": "2.10.0"
      }
    }
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	byName := dependencyRowsByName(rows)

	if got, want := byName["Newtonsoft.Json"]["line_number"], 5; got != want {
		t.Errorf("Newtonsoft.Json line_number = %v, want %d", got, want)
	}
	if got, want := byName["Serilog"]["line_number"], 10; got != want {
		t.Errorf("Serilog line_number = %v, want %d", got, want)
	}
}

// TestSwiftPackageResolvedEmitsRealLineNumbers pins issue #5329 for
// Package.resolved, whose "pins" array has no key to look up by name, so
// lines are correlated by loop index instead.
func TestSwiftPackageResolvedEmitsRealLineNumbers(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "Package.resolved", `{
  "version": 2,
  "pins": [
    {
      "identity": "swift-log",
      "kind": "remoteSourceControl",
      "location": "https://github.com/apple/swift-log.git",
      "state": {
        "revision": "abc123",
        "version": "1.5.3"
      }
    },
    {
      "identity": "swift-nio",
      "kind": "remoteSourceControl",
      "location": "https://github.com/apple/swift-nio.git",
      "state": {
        "revision": "def456",
        "version": "2.55.0"
      }
    }
  ]
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	if len(rows) != 2 {
		t.Fatalf("len(variables) = %d, want 2", len(rows))
	}
	if got, want := rows[0]["line_number"], 4; got != want {
		t.Errorf("rows[0][line_number] = %v, want %d", got, want)
	}
	if got, want := rows[1]["line_number"], 13; got != want {
		t.Errorf("rows[1][line_number] = %v, want %d", got, want)
	}
}
