// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// oldParseJSONMetadataKeys reconstructs the value the JSON adapter assigned to
// payload["json_metadata"]["top_level_keys"] BEFORE issue #4873, when Parse
// ran the full ordered walk (unmarshalOrderedJSONObject) unconditionally for
// every JSON object. It is the reference "old" side of the equivalence proof:
// it normalizes the file exactly as Parse does, then derives top-level keys
// the pre-change way.
func oldParseJSONMetadataKeys(t *testing.T, path string) ([]string, bool) {
	t.Helper()

	source, err := shared.ReadSource(path)
	if err != nil {
		t.Fatalf("ReadSource(%q) error = %v, want nil", path, err)
	}
	normalized := normalizeJSONSource(source, filepath.Base(path))
	entries, err := unmarshalOrderedJSONObject([]byte(normalized))
	if err != nil {
		// Pre-change code left json_metadata at its base default (empty keys)
		// when the ordered walk failed; signal that so the assertion matches.
		return nil, false
	}
	return orderedJSONKeys(entries), true
}

func parsedTopLevelKeys(t *testing.T, payload map[string]any) []string {
	t.Helper()

	metadata, ok := payload["json_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("json_metadata = %T, want map[string]any", payload["json_metadata"])
	}
	keys, ok := metadata["top_level_keys"].([]string)
	if !ok {
		t.Fatalf("json_metadata.top_level_keys = %T, want []string", metadata["top_level_keys"])
	}
	return keys
}

// TestParseJSONMetadataUnchangedAcrossConsumerPaths is the end-to-end
// equivalence proof for issue #4873. The change made the full ordered walk
// conditional: only package.json/composer.json/tsconfig*.json still run it;
// every other file now derives json_metadata.top_level_keys from the cheaper
// topLevelJSONKeyOrder scan. json_metadata.top_level_keys is the ONLY payload
// field whose computation path changed for those files — every other bucket is
// built from the unchanged `any` decode (`object`), and none of the skipped
// filenames' dispatch branches take a topLevelEntries argument (enforced by
// TestJSONFilenameNeedsOrderedEntriesMirrorsParseDispatch). So proving the new
// json_metadata equals its pre-change value on each consumer path proves the
// full Parse payload is byte-identical old-vs-new.
//
// Covers all three consumer paths the routing change could regress plus the
// duplicate/reordered-key edge case:
//   - package-lock / NuGet lockfile dependency extraction (ordered keys),
//   - CloudFormation dispatch,
//   - dbt dispatch,
//   - a plain JSON object with reordered and duplicate top-level keys.
func TestParseJSONMetadataUnchangedAcrossConsumerPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		filename string
		body     string
	}{
		{
			name:     "package-lock dependency extraction",
			filename: "package-lock.json",
			body: `{
  "name": "eshu",
  "lockfileVersion": 3,
  "packages": {
    "": {"dependencies": {"vite": "^5.4.11"}},
    "node_modules/vite": {"version": "5.4.21"},
    "node_modules/rollup": {"version": "4.34.9"}
  }
}`,
		},
		{
			name:     "nuget lockfile dependency extraction",
			filename: "packages.lock.json",
			body: `{
  "version": 1,
  "dependencies": {
    "net8.0": {
      "Newtonsoft.Json": {"type": "Direct", "requested": "[13.0.3, )", "resolved": "13.0.3"}
    }
  }
}`,
		},
		{
			name:     "composer lockfile dependency extraction",
			filename: "composer.lock",
			body: `{
  "packages": [
    {"name": "vendor/pkg", "version": "1.2.3"}
  ],
  "packages-dev": []
}`,
		},
		{
			name:     "cloudformation dispatch",
			filename: "template.json",
			body: `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Parameters": {"StageName": {"Type": "String"}},
  "Resources": {"HelloFunction": {"Type": "AWS::Lambda::Function", "Properties": {}}},
  "Outputs": {"ApiUrl": {"Value": "https://example.com"}}
}`,
		},
		{
			name:     "dbt dispatch",
			filename: "dbt_manifest.json",
			body: `{
  "metadata": {"dbt_version": "1.7.0"},
  "sources": {},
  "macros": {},
  "nodes": {
    "model.analytics.order_metrics": {
      "resource_type": "model",
      "database": "analytics",
      "schema": "public",
      "identifier": "order_metrics",
      "name": "order_metrics",
      "compiled_code": "select id from raw.public.orders",
      "depends_on": {"nodes": []},
      "columns": {"id": {"name": "id"}}
    }
  }
}`,
		},
		{
			name:     "plain object with reordered and duplicate keys",
			filename: "config.json",
			body:     `{"zeta": 1, "alpha": 2, "zeta": 3, "middle": {"nested": true}}`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := writeJSONTestFile(t, testCase.filename, testCase.body)

			payload, err := Parse(path, false, shared.Options{}, Config{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			gotKeys := parsedTopLevelKeys(t, payload)

			wantKeys, ok := oldParseJSONMetadataKeys(t, path)
			if !ok {
				t.Fatalf("pre-change ordered walk failed for %q; equivalence undefined", testCase.filename)
			}
			if !reflect.DeepEqual(gotKeys, wantKeys) {
				t.Fatalf("json_metadata.top_level_keys new=%#v, old=%#v (0/0 equivalence broken for %q)", gotKeys, wantKeys, testCase.filename)
			}
		})
	}
}
