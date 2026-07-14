// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestTopLevelJSONKeyOrderMatchesOrderedWalk pins the reconstruction claim
// behind issue #4873: for any document, the cheap key-order-only scan must
// return exactly the same top-level key sequence as the full ordered walk
// (unmarshalOrderedJSONObject), on a real, large, representative lockfile as
// well as small hand-written documents covering nested objects, arrays,
// scalars, and an empty object.
func TestTopLevelJSONKeyOrderMatchesOrderedWalk(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		json string
	}{
		{name: "empty object", json: `{}`},
		{name: "scalars and null", json: `{"a":1,"b":"two","c":true,"d":null,"e":-3.5}`},
		{name: "nested object and array", json: `{"z":{"nested":{"deep":[1,2,3]}},"a":[{"k":"v"},1,"s"]}`},
		{name: "duplicate-shaped values", json: `{"packages":{"x":{"version":"1.0.0"}},"dependencies":{"x":"^1.0.0"},"name":"demo"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			entries, err := unmarshalOrderedJSONObject([]byte(testCase.json))
			if err != nil {
				t.Fatalf("unmarshalOrderedJSONObject() error = %v, want nil", err)
			}
			got, err := topLevelJSONKeyOrder([]byte(testCase.json))
			if err != nil {
				t.Fatalf("topLevelJSONKeyOrder() error = %v, want nil", err)
			}
			if want := orderedJSONKeys(entries); !reflect.DeepEqual(got, want) {
				t.Fatalf("topLevelJSONKeyOrder() = %#v, want %#v", got, want)
			}
		})
	}
}

// TestTopLevelJSONKeyOrderMatchesLargeFixture proves the reconstruction
// claim on a real, large package-lock.json (the motivating case for issue
// #4873): the cheap scan's key order must match the full ordered walk, and
// the `object` any-decode used downstream must be untouched by which scan
// ran.
func TestTopLevelJSONKeyOrderMatchesLargeFixture(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "large-package-lock.json"))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v, want nil", err)
	}

	entries, err := unmarshalOrderedJSONObject(data)
	if err != nil {
		t.Fatalf("unmarshalOrderedJSONObject() error = %v, want nil", err)
	}
	got, err := topLevelJSONKeyOrder(data)
	if err != nil {
		t.Fatalf("topLevelJSONKeyOrder() error = %v, want nil", err)
	}
	if want := orderedJSONKeys(entries); !reflect.DeepEqual(got, want) {
		t.Fatalf("topLevelJSONKeyOrder() = %#v, want %#v", got, want)
	}
	if len(got) == 0 {
		t.Fatalf("topLevelJSONKeyOrder() returned no keys, want a non-empty top-level key set")
	}
}

func TestTopLevelJSONKeyOrderRejectsNonObjectRoot(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`[1,2,3]`, `"just a string"`, `42`, `true`, `null`} {
		if _, err := topLevelJSONKeyOrder([]byte(input)); err == nil {
			t.Fatalf("topLevelJSONKeyOrder(%q) error = nil, want non-object-root error", input)
		}
	}
}

func TestTopLevelJSONKeyOrderRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	if _, err := topLevelJSONKeyOrder([]byte(`{"a":1,`)); err == nil {
		t.Fatal("topLevelJSONKeyOrder() error = nil, want malformed JSON error")
	}
}

// TestJSONFilenameNeedsOrderedEntriesMirrorsParseDispatch pins the routing
// table jsonFilenameNeedsOrderedEntries must stay in lockstep with: exactly
// package.json, composer.json, and tsconfig*.json read topLevelEntries in
// Parse's switch (dependencyVariablesWithScope, jsonScriptFunctions,
// tsconfigVariables). Every other filename -- including the dedicated
// lockfile parsers that never take a topLevelEntries parameter -- must route
// to the cheap top-level-keys-only scan.
func TestJSONFilenameNeedsOrderedEntriesMirrorsParseDispatch(t *testing.T) {
	t.Parallel()

	needsEntries := []string{"package.json", "composer.json", "tsconfig.json", "tsconfig.base.json", "TSCONFIG.PROD.JSON"}
	for _, filename := range needsEntries {
		if !jsonFilenameNeedsOrderedEntries(filename) {
			t.Errorf("jsonFilenameNeedsOrderedEntries(%q) = false, want true", filename)
		}
	}

	noEntries := []string{
		"package-lock.json", "packages.lock.json", "composer.lock", "pipfile.lock",
		"package.resolved", "template.json", "dbt_manifest.json", "turbo.jsonc",
		"data.min.json", "config.json",
	}
	for _, filename := range noEntries {
		if jsonFilenameNeedsOrderedEntries(filename) {
			t.Errorf("jsonFilenameNeedsOrderedEntries(%q) = true, want false", filename)
		}
	}
}

// TestOrderedJSONNestedObjectUnaffectedByKeyOrderOnlyScan confirms
// unmarshalOrderedJSONObject's own nested-entry re-decode (used by
// orderedJSONNestedObject for compilerOptions.paths) is untouched: it still
// carries decodable json.RawMessage values, unlike topLevelJSONKeyOrder's
// output which intentionally discards them.
func TestOrderedJSONNestedObjectUnaffectedByKeyOrderOnlyScan(t *testing.T) {
	t.Parallel()

	data := []byte(`{"compilerOptions":{"paths":{"@z/*":["z"],"@a/*":["a"]}}}`)
	entries, err := unmarshalOrderedJSONObject(data)
	if err != nil {
		t.Fatalf("unmarshalOrderedJSONObject() error = %v, want nil", err)
	}
	nested, ok, err := orderedJSONNestedObject(entries, "compilerOptions")
	if err != nil || !ok {
		t.Fatalf("orderedJSONNestedObject() = (%v, %v, %v), want entries, true, nil", nested, ok, err)
	}
	pathsNested, ok, err := orderedJSONNestedObject(nested, "paths")
	if err != nil || !ok {
		t.Fatalf("orderedJSONNestedObject(paths) = (%v, %v, %v), want entries, true, nil", pathsNested, ok, err)
	}
	if got, want := orderedJSONKeys(pathsNested), []string{"@z/*", "@a/*"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths key order = %#v, want %#v", got, want)
	}
}

// TestJSONValueSkipperIsANoOpUnmarshaler proves jsonValueSkipper never
// errors and never needs to retain the bytes it is handed, so
// topLevelJSONKeyOrder's use of it is purely a skip, not a silent data drop
// that could be mistaken for success.
func TestJSONValueSkipperIsANoOpUnmarshaler(t *testing.T) {
	t.Parallel()

	var skip jsonValueSkipper
	for _, input := range []string{`{"anything":[1,2,3]}`, `[1,2,3]`, `"s"`, `42`, `null`, `true`} {
		if err := skip.UnmarshalJSON([]byte(input)); err != nil {
			t.Fatalf("UnmarshalJSON(%q) error = %v, want nil", input, err)
		}
	}
}
