// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestCargoRuntimeEvidenceUsesDecodedPackageAndSourceLine(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		want         []shared.RuntimeValue
		wantRejected int
	}{
		{"package", "# rust-version = \"9\"\n[workspace.package]\nrust-version = \"8\"\n[package]\nname = \"demo\"\nrust-version = \"1.78.0\"\n", []shared.RuntimeValue{{SourceKind: "cargo_package_rust_version", Runtime: "rust", Value: "1.78.0", Line: 6}}, 0},
		{"dotted", "package.name = \"demo\"\npackage.rust-version = \"1.79\"\n", []shared.RuntimeValue{{SourceKind: "cargo_package_rust_version", Runtime: "rust", Value: "1.79", Line: 2}}, 0},
		{"credential", "[package]\nrust-version = \"https://user:password@example.com\"\n", nil, 1},
		{"workspace only", "[workspace.package]\nrust-version = \"1.78\"\n", nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Cargo.toml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			payload, err := ParseCargoDependencyFile(filepath.Dir(path), path, false)
			if err != nil {
				t.Fatal(err)
			}
			evidence := payload["rust_runtime_evidence"].(shared.RuntimeEvidence)
			if len(evidence.Values) != len(tc.want) || evidence.Rejected != tc.wantRejected || evidence.Digest != shared.RuntimeDigest([]byte(tc.body)) {
				t.Fatalf("evidence = %+v", evidence)
			}
			for i, want := range tc.want {
				if evidence.Values[i] != want {
					t.Fatalf("value = %+v, want %+v", evidence.Values[i], want)
				}
			}
		})
	}
}
