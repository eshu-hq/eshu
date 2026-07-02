// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPreScanMatchesParseDeclarationNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		body string
		keys []string
	}{
		{
			name: "javascript",
			file: "sample.js",
			body: `class Greeter {}
function hello() {}
const world = () => world;
const handlers = { onStart() {}, onStop: function onStop() {} };
module.exports.encode = async data => String(data);
exports.decorate = function decorate(value) { return value; };
export default function exported() {}
`,
			keys: []string{"functions", "classes"},
		},
		{
			name: "typescript",
			file: "sample.ts",
			body: `interface Reader {}
abstract class ConfigReader implements Reader {
    load(): void {}
}
function loadConfig() {}
const parseConfig = function parseConfig() {};
`,
			keys: []string{"functions", "classes", "interfaces"},
		},
		{
			name: "php",
			file: "sample.php",
			body: `<?php
interface Reader {}
trait Logs {}
class Greeter implements Reader {
    public function hello(): void {}
}
$anon = new class {
    public function handle(): void {}
};
function bootstrap() {}
`,
			keys: []string{"functions", "classes", "traits", "interfaces"},
		},
		{
			name: "python",
			file: "service.py",
			body: `"""service module"""
class Greeter:
    def build(self):
        return self

class Runner:
    pass

def hello(name):
    return name
`,
			keys: []string{"functions", "classes", "modules"},
		},
	}

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			sourcePath := filepath.Join(repoRoot, tt.file)
			writeTestFile(t, sourcePath, tt.body)

			payload, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
			if err != nil {
				t.Fatalf("ParsePath() error = %v, want nil", err)
			}
			want := prescanMapFromPayload(sourcePath, payload, tt.keys...)

			got, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
			if err != nil {
				t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
			}
			if !prescanMapsEqual(got, want) {
				t.Fatalf("PreScanRepositoryPaths() = %#v, want names from ParsePath %#v", got, want)
			}
		})
	}
}

func prescanMapFromPayload(sourcePath string, payload map[string]any, keys ...string) map[string][]string {
	results := make(map[string][]string)
	for _, key := range keys {
		items, _ := payload[key].([]map[string]any)
		for _, item := range items {
			name, _ := item["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			results[filepath.Clean(name)] = append(results[filepath.Clean(name)], sourcePath)
		}
	}
	for _, paths := range results {
		slices.Sort(paths)
	}
	return results
}
