// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestDartFvmrcEvidenceShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body, want string
		line             int
		state            string
		rejected         int
	}{
		{"exact", `{"flutter":"3.22.0"}`, "3.22.0", 1, "parsed_values", 0},
		{"multiline", "{\n  \"flutter\": \"3.22.0\"\n}", "3.22.0", 2, "parsed_values", 0},
		{"absent", `{"flutterUrl":"https://example.com"}`, "", 0, "parsed_empty", 0},
		{"nonstring", `{"flutter":{"channel":"stable"}}`, "", 0, "parsed_empty", 0},
		{"credential", `{"flutter":"https://user:password@example.invalid"}`, "", 0, "parsed_empty", 1},
		{"malformed", `{"flutter":`, "", 0, "unavailable", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := ExtractDartFvmrcEvidence([]byte(tc.body))
			if e.State != tc.state || e.Rejected != tc.rejected || len(e.Values) != btoi(tc.want != "") {
				t.Fatalf("evidence = %+v", e)
			}
			if tc.want != "" {
				got := e.Values[0]
				if got.Value != tc.want || got.Line != tc.line || got.SourceKind != "dart_fvmrc_flutter" {
					t.Fatalf("value = %+v", e.Values)
				}
			}
		})
	}
}

func TestDartFvmrcPayloadCarriesPrivateEvidence(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".fvmrc")
	if err := os.WriteFile(path, []byte("{\n  \"flutter\": \"3.22.0\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := payload["dart_runtime_evidence"].(shared.RuntimeEvidence)
	if !ok || len(evidence.Values) != 1 || evidence.Values[0].Line != 2 {
		t.Fatalf("runtime evidence = %#v", payload["dart_runtime_evidence"])
	}
}
