// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

type nodeTypeScriptFixtureMatrix struct {
	Cases []nodeTypeScriptFixtureCase `json:"cases"`
}

type nodeTypeScriptFixtureCase struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	EntityType string   `json:"entity_type"`
	SourceFile string   `json:"source_file"`
	RootKinds  []string `json:"root_kinds"`
}

func TestDefaultEngineParsePathNodeTypeScriptFixtureExpectedRoots(t *testing.T) {
	t.Parallel()

	matrix := loadNodeTypeScriptFixtureMatrix(t)
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	parsed := map[string]map[string]any{}
	for _, item := range matrix.Cases {
		if len(item.RootKinds) == 0 {
			continue
		}
		item := item
		t.Run(item.ID, func(t *testing.T) {
			payload, ok := parsed[item.SourceFile]
			if !ok {
				sourcePath := filepath.Join(nodeTypeScriptFixtureRoot(), item.SourceFile)
				payload, err = engine.ParsePath(nodeTypeScriptFixtureRoot(), sourcePath, false, parser.Options{})
				if err != nil {
					t.Fatalf("ParsePath(%s) error = %v, want nil", item.SourceFile, err)
				}
				parsed[item.SourceFile] = payload
			}

			entity := nodeTypeScriptFixtureEntity(t, payload, item)
			for _, rootKind := range item.RootKinds {
				assertParserStringSliceContains(t, entity, "dead_code_root_kinds", rootKind)
			}
		})
	}
}

func loadNodeTypeScriptFixtureMatrix(t *testing.T) nodeTypeScriptFixtureMatrix {
	t.Helper()

	path := filepath.Join(nodeTypeScriptFixtureRoot(), "expected.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	var matrix nodeTypeScriptFixtureMatrix
	if err := json.Unmarshal(data, &matrix); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", path, err)
	}
	return matrix
}

// nodeTypeScriptFixtureRoot resolves the fixture directory relative to the
// test binary's working directory (go test runs with cwd set to the package
// directory). The javascript package sits one directory deeper than the
// parent parser package this test relocated from, so the walk up to the repo
// root takes four ".." elements instead of the pre-move three.
func nodeTypeScriptFixtureRoot() string {
	return filepath.Join("..", "..", "..", "..", "tests", "fixtures", "dead-code", "node-typescript")
}

func nodeTypeScriptFixtureEntity(t *testing.T, payload map[string]any, item nodeTypeScriptFixtureCase) map[string]any {
	t.Helper()

	switch item.EntityType {
	case "Class":
		return assertBucketItemByName(t, payload, "classes", item.Name)
	default:
		return assertFunctionByName(t, payload, item.Name)
	}
}
