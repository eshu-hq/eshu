// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestGoldenFixturesConform(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		wantErr        bool
		wantDuplicates int
		wantRedactions int
	}{
		{name: "complete"},
		{name: "unchanged"},
		{name: "partial"},
		{name: "retryable"},
		{name: "terminal"},
		{name: "duplicate", wantDuplicates: 1},
		{name: "conflict", wantErr: true},
		{name: "tombstone"},
		{name: "redaction", wantRedactions: 1},
	}
	validator := NewValidator(testContract())
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := readFixture(t, tc.name)
			report, err := validator.ValidateResult(result)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateResult(%s) error = nil, want non-nil", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateResult(%s) error = %v, want nil", tc.name, err)
			}
			if report.DuplicateCount != tc.wantDuplicates {
				t.Fatalf("DuplicateCount = %d, want %d", report.DuplicateCount, tc.wantDuplicates)
			}
			if report.RedactionCount != tc.wantRedactions {
				t.Fatalf("RedactionCount = %d, want %d", report.RedactionCount, tc.wantRedactions)
			}
		})
	}
}

func TestValidationRejectsUndeclaredAndUnsafeFacts(t *testing.T) {
	t.Parallel()

	result := readFixture(t, "complete")
	tests := []struct {
		name   string
		mutate func(*Result)
	}{
		{
			name: "undeclared fact kind",
			mutate: func(result *Result) {
				result.Facts[0].Kind = "other.publisher.fact"
			},
		},
		{
			name: "unsupported schema version",
			mutate: func(result *Result) {
				result.Facts[0].SchemaVersion = "2.0.0"
			},
		},
		{
			name: "unknown confidence",
			mutate: func(result *Result) {
				result.Facts[0].SourceConfidence = SourceConfidenceUnknown
			},
		},
		{
			name: "blank stable key",
			mutate: func(result *Result) {
				result.Facts[0].StableKey = ""
			},
		},
		{
			name: "source ref scope mismatch",
			mutate: func(result *Result) {
				result.Facts[0].SourceRef.ScopeID = "repo:other"
			},
		},
		{
			name: "credential-bearing source URI",
			mutate: func(result *Result) {
				result.Facts[0].SourceRef.URI = "https://token@example.com/source"
			},
		},
	}
	validator := NewValidator(testContract())
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			candidate := cloneResult(result)
			tt.mutate(&candidate)
			if _, err := validator.ValidateResult(candidate); err == nil {
				t.Fatalf("ValidateResult() error = nil, want non-nil")
			}
		})
	}
}

func TestJSONSchemaMatchesGolden(t *testing.T) {
	t.Parallel()

	got, err := JSONSchema()
	if err != nil {
		t.Fatalf("JSONSchema() error = %v, want nil", err)
	}
	want, err := os.ReadFile(filepath.Join("schema", "collector-sdk-v1alpha1.schema.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(schema) error = %v, want nil", err)
	}
	if !jsonEqual(got, want) {
		t.Fatalf("JSONSchema() did not match golden schema\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestSDKModuleDoesNotImportEshuInternals(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath := importPath(spec)
			if importPath == "github.com/eshu-hq/eshu/go/internal" ||
				bytes.Contains([]byte(importPath), []byte("github.com/eshu-hq/eshu/go/internal/")) {
				t.Fatalf("SDK module imports Eshu internal package %q", importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.WalkDir() error = %v, want nil", err)
	}
}

func readFixture(t *testing.T, name string) Result {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "fixtures", name+".json"))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v, want nil", name, err)
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v, want nil", name, err)
	}
	return result
}

func testContract() Contract {
	return Contract{
		ProtocolVersion: ProtocolVersionV1Alpha1,
		Facts: []FactDeclaration{
			{
				Kind:             "dev.eshu.examples.scorecard.snapshot",
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []SourceConfidence{SourceConfidenceReported},
				TombstoneAllowed: true,
			},
			{
				Kind:             "dev.eshu.examples.scorecard.warning",
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []SourceConfidence{SourceConfidenceReported},
			},
		},
	}
}

func cloneResult(result Result) Result {
	raw, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	var cloned Result
	if err := json.Unmarshal(raw, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func jsonEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	leftCanonical, _ := json.Marshal(leftValue)
	rightCanonical, _ := json.Marshal(rightValue)
	return bytes.Equal(leftCanonical, rightCanonical)
}

func importPath(spec *ast.ImportSpec) string {
	var path string
	if err := json.Unmarshal([]byte(spec.Path.Value), &path); err != nil {
		return spec.Path.Value
	}
	return path
}
