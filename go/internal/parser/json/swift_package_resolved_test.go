// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestSwiftPackageResolvedEmitsOnlyVersionedRemoteDependencies(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "Package.resolved", `{
  "pins": [
    {
      "identity": "swift-argument-parser",
      "kind": "remoteSourceControl",
      "location": "https://github.com/apple/swift-argument-parser.git",
      "state": {
        "revision": "0123456789abcdef0123456789abcdef01234567",
        "version": "1.2.3"
      }
    },
    {
      "identity": "swift-syntax",
      "kind": "remoteSourceControl",
      "location": "https://github.com/swiftlang/swift-syntax.git",
      "state": {
        "branch": "main",
        "revision": "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
      }
    },
    {
      "identity": "local-helper",
      "kind": "localSourceControl",
      "location": "../local-helper",
      "state": {
        "version": "0.1.0"
      }
    }
  ],
  "version": 2
}`)
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	rowsByName := dependencyRowsByName(rows)

	row, ok := rowsByName["github.com/apple/swift-argument-parser"]
	if !ok {
		t.Fatalf("swift-argument-parser dependency missing from %#v", rows)
	}
	if row["value"] != "1.2.3" {
		t.Fatalf("value = %#v, want exact Package.resolved version", row["value"])
	}
	if row["config_kind"] != "dependency" || row["package_manager"] != "swift" {
		t.Fatalf("row metadata = %#v, want Swift dependency evidence", row)
	}
	if row["section"] != "Package.resolved" || row["lockfile"] != true {
		t.Fatalf("row provenance = %#v, want Package.resolved lockfile evidence", row)
	}
	if row["package_identity"] != "swift-argument-parser" {
		t.Fatalf("package_identity = %#v, want SwiftPM identity", row["package_identity"])
	}
	if row["source_location"] != "https://github.com/apple/swift-argument-parser.git" {
		t.Fatalf("source_location = %#v, want original remote location", row["source_location"])
	}
	if _, ok := rowsByName["github.com/swiftlang/swift-syntax"]; ok {
		t.Fatalf("revision-only Swift pin emitted dependency evidence: %#v", rowsByName["github.com/swiftlang/swift-syntax"])
	}
	if _, ok := rowsByName["local-helper"]; ok {
		t.Fatalf("local Swift pin emitted remote dependency evidence: %#v", rowsByName["local-helper"])
	}
}

func TestSwiftPackageResolvedSanitizesCredentialedSourceLocation(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "Package.resolved", `{
  "pins": [
    {
      "identity": "swift-crypto",
      "kind": "remoteSourceControl",
      "location": "https://token:secret@github.com/apple/swift-crypto.git?token=secret&ref=main#fragment",
      "state": {
        "revision": "0123456789abcdef0123456789abcdef01234567",
        "version": "4.3.0"
      }
    }
  ],
  "version": 2
}`)
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	rowsByName := dependencyRowsByName(rows)

	row, ok := rowsByName["github.com/apple/swift-crypto"]
	if !ok {
		t.Fatalf("swift-crypto dependency missing from %#v", rows)
	}
	if got, want := row["source_location"], "https://github.com/apple/swift-crypto.git"; got != want {
		t.Fatalf("source_location = %#v, want sanitized URL %#v", got, want)
	}
}

func TestSwiftPackageResolvedUnsupportedFormatVersionEmitsNoDependencies(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "Package.resolved", `{
  "pins": [
    {
      "identity": "swift-argument-parser",
      "kind": "remoteSourceControl",
      "location": "https://github.com/apple/swift-argument-parser.git",
      "state": {
        "version": "1.2.3"
      }
    }
  ],
  "version": 1
}`)
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	for _, row := range rows {
		if row["config_kind"] == "dependency" {
			t.Fatalf("unsupported Package.resolved format emitted dependency evidence: %#v", row)
		}
	}
}
