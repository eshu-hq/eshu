// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cabal

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func cabalValues(t *testing.T, body string) (map[int][]string, shared.RuntimeEvidence) {
	t.Helper()
	evidence := ExtractCabalRuntimeEvidence([]byte(body))
	got := make(map[int][]string)
	for _, v := range evidence.Values {
		if v.SourceKind != sourceKindCabalTestedWith || v.Runtime != "haskell" {
			t.Fatalf("value = %+v, want kind %q runtime %q", v, sourceKindCabalTestedWith, "haskell")
		}
		got[v.Line] = append(got[v.Line], v.Value)
	}
	return got, evidence
}

// TestExtractCabalTestedWith pins literal tested-with extraction.
func TestExtractCabalTestedWith(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		want       map[int][]string
		rejected   int
	}{
		{"single line", "cabal-version: 2.4\nname: demo\nversion: 0.1.0.0\ntested-with: GHC==9.4.8, GHC==9.6.4\n", map[int][]string{4: {"GHC==9.4.8", "GHC==9.6.4"}}, 0},
		{"multiline", "name: demo\ntested-with: GHC==9.4.8,\n  GHC==9.6.4\nversion: 0.1.0.0\n", map[int][]string{2: {"GHC==9.4.8"}, 3: {"GHC==9.6.4"}}, 0},
		{"case and comment", "name: demo\nTested-With: GHC==9.4.8 -- nightly only\n", map[int][]string{2: {"GHC==9.4.8"}}, 0},
		{"credential drops counted", "name: demo\ntested-with: GHC==9.4.8, https://user:secret@example.invalid/ghc\n", map[int][]string{2: {"GHC==9.4.8"}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, evidence := cabalValues(t, tc.body)
			if evidence.State != "parsed_values" || evidence.Rejected != tc.rejected {
				t.Fatalf("state = %q rejected = %d, want parsed_values %d", evidence.State, evidence.Rejected, tc.rejected)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestExtractCabalNonLiteral pins fail-closed and empty reads.
func TestExtractCabalNonLiteral(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, state string }{
		{"nul byte", "name: demo\ntested-with: GHC==9.4.8\x00\n", "unavailable"},
		{"duplicate field", "tested-with: GHC==9.4.8\ntested-with: GHC==9.6.4\n", "unavailable"},
		{"nested field", "library\n  tested-with: GHC==9.4.8\n", "unavailable"},
		{"nested field after value", "tested-with: GHC==9.4.8\n  tested-with: GHC==9.6.4\n", "unavailable"},
		{"package conditional", "if impl(ghc >= 9.4)\n  ghc-options: -Werror\n", "unavailable"},
		{"conditional continuation", "tested-with: GHC==9.4.8\n  if os(windows)\n", "unavailable"},
		{"empty value", "name: demo\ntested-with:\nversion: 0.1.0.0\n", "unavailable"},
		{"no field", "name: demo\nversion: 0.1.0.0\n", "parsed_empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			evidence := ExtractCabalRuntimeEvidence([]byte(tc.body))
			if evidence.State != tc.state || len(evidence.Values) != 0 {
				t.Fatalf("got %+v, want %s with no values", evidence, tc.state)
			}
		})
	}
}

// TestParseCabalPayload pins the native payload: cabal tag, never haskell.
func TestParseCabalPayload(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "demo.cabal")
	if err := os.WriteFile(path, []byte("name: demo\ntested-with: GHC==9.4.8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if payload["lang"] != "cabal" {
		t.Fatalf("lang = %v, want cabal", payload["lang"])
	}
	evidence, ok := payload["cabal_runtime_evidence"].(shared.RuntimeEvidence)
	if !ok || len(evidence.Values) != 1 || evidence.Values[0].Value != "GHC==9.4.8" {
		t.Fatalf("evidence = %+v, want one GHC spec", payload["cabal_runtime_evidence"])
	}
}
