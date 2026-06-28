// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schema

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// updateGolden rewrites the committed schema JSON files from the Go builder.
// Run `go test ./internal/replay/schema -run TestCassetteSchemaMatchesGolden
// -update` after changing the cassette format or the builder, then commit the
// result. The same flag name is the repo convention for golden regeneration.
var updateGolden = flag.Bool("update", false, "rewrite committed cassette-format schema golden files")

// goldenSchemaPaths are the two committed copies of the generated schema: the
// canonical copy beside the builder, and the SDK mirror that ships the contract
// to external collector authors. Both are kept byte-identical to the builder.
func goldenSchemaPaths() []string {
	return []string{
		"cassette-format.v1.schema.json",
		filepath.Join("..", "..", "..", "..", "sdk", "go", "collector", "schema", "cassette-format.v1.schema.json"),
	}
}

func TestCassetteSchemaMatchesGolden(t *testing.T) {
	got, err := CassetteFormatV1()
	if err != nil {
		t.Fatalf("CassetteFormatV1() error = %v, want nil", err)
	}

	for _, path := range goldenSchemaPaths() {
		path := path
		t.Run(path, func(t *testing.T) {
			if *updateGolden {
				if err := os.WriteFile(path, got, 0o644); err != nil { //nolint:gosec // committed schema artifact, world-readable by design
					t.Fatalf("write golden %q: %v", path, err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %q: %v (re-run with -update to generate)", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("schema %q is stale; re-run: go test ./internal/replay/schema -run TestCassetteSchemaMatchesGolden -update", path)
			}
		})
	}
}

// TestCassetteSchemaIsDeterministic proves the builder emits byte-identical
// output across calls (sorted keys, fixed indent). A non-deterministic builder
// would make the matches-golden gate flap.
func TestCassetteSchemaIsDeterministic(t *testing.T) {
	first, err := CassetteFormatV1()
	if err != nil {
		t.Fatalf("CassetteFormatV1() error = %v", err)
	}
	second, err := CassetteFormatV1()
	if err != nil {
		t.Fatalf("CassetteFormatV1() error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("CassetteFormatV1() is not deterministic across calls")
	}
}

// TestSchemaPropertiesMatchCassetteStructs is the cross-link gate: every object
// in the schema must declare exactly the JSON keys its backing cassette struct
// serializes. This binds the schema to format.go — adding a field to the struct
// without adding it to the builder (or vice versa) fails here, before any
// cassette is even loaded.
func TestSchemaPropertiesMatchCassetteStructs(t *testing.T) {
	cases := []struct {
		name   string
		schema map[string]any
		want   map[string]struct{}
	}{
		{"file", cassetteFormatSchema(), fileKeys},
		{"scope", scopeSchema(), scopeKeys},
		{"fact", factSchema(), factKeys},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			props, ok := tc.schema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("schema %q has no properties map", tc.name)
			}
			got := make(map[string]struct{}, len(props))
			for k := range props {
				got[k] = struct{}{}
			}
			if !sameKeySet(got, tc.want) {
				t.Fatalf("schema %q properties = %v, want struct keys %v", tc.name, keys(got), keys(tc.want))
			}
		})
	}
}

// TestSchemaRequiredAreDeclaredProperties guards against a required field that
// is not also a declared property (which would make the schema self-contradict
// additionalProperties:false).
func TestSchemaRequiredAreDeclaredProperties(t *testing.T) {
	for name, obj := range map[string]map[string]any{
		"file":  cassetteFormatSchema(),
		"scope": scopeSchema(),
		"fact":  factSchema(),
	} {
		props, _ := obj["properties"].(map[string]any)
		required, _ := obj["required"].([]string)
		for _, r := range required {
			if _, ok := props[r]; !ok {
				t.Errorf("schema %q requires %q but does not declare it as a property", name, r)
			}
		}
	}
}

func sameKeySet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
