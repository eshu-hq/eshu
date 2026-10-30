// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestSwiftToolsVersionEvidenceReadsTheFirstMarkerOnce pins the read-once
// typed-evidence handoff for the swift-tools-version (#9308 Others lane ii):
// the value comes from the same accepted bytes with its real source line, a
// missing marker is absent, a marker without a value is malformed, and a
// credential-shaped value is rejected before emission.
func TestSwiftToolsVersionEvidenceReadsTheFirstMarkerOnce(t *testing.T) {
	t.Parallel()

	body := "// swift-tools-version: 5.9\nimport PackageDescription\n// swift-tools-version: 6.0\n"
	evidence := ExtractSwiftToolsVersionEvidence([]byte(body))
	want := []shared.RuntimeValue{{SourceKind: "swift_tools_version", Runtime: "swift", Value: "5.9", Line: 1}}
	if len(evidence.Values) != len(want) || evidence.Rejected != 0 || evidence.State != "parsed_values" {
		t.Fatalf("evidence = %+v, want %+v", evidence, want)
	}
	for i, value := range want {
		if evidence.Values[i] != value {
			t.Fatalf("value[%d] = %+v, want %+v", i, evidence.Values[i], value)
		}
	}
	if evidence.Digest != shared.RuntimeDigest([]byte(body)) {
		t.Fatal("evidence is not bound to the exact source bytes")
	}

	credential := ExtractSwiftToolsVersionEvidence([]byte("// swift-tools-version: https://user:password@example.invalid/5.9\n"))
	if len(credential.Values) != 0 || credential.Rejected != 1 {
		t.Fatalf("credential evidence = %+v, want one rejection and no value", credential)
	}

	absent := ExtractSwiftToolsVersionEvidence([]byte("import PackageDescription\n"))
	if len(absent.Values) != 0 || absent.Rejected != 0 || absent.State != "parsed_empty" {
		t.Fatalf("absent evidence = %+v, want parsed_empty with no values", absent)
	}

	malformed := ExtractSwiftToolsVersionEvidence([]byte("// swift-tools-version:\n"))
	if malformed.State != "unavailable" {
		t.Fatalf("malformed evidence = %+v, want unavailable", malformed)
	}

	bare := ExtractSwiftToolsVersionEvidence([]byte("// swift-tools-version 5.9\n"))
	if len(bare.Values) != 0 || bare.State != "parsed_empty" {
		t.Fatalf("bare mention evidence = %+v, want parsed_empty with no values", bare)
	}

	over := ExtractSwiftToolsVersionEvidence([]byte("// swift-tools-version: 5.9\n" + strings.Repeat("// filler\n", 1<<20)))
	if over.State != "over_cap" {
		t.Fatalf("over-cap evidence = %+v, want over_cap", over)
	}
}
