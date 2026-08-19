// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadSubmodulePinFamilyOduRejectsUnknownJSONField proves the cassette
// decoder catches a typo (e.g. "fact_knd" instead of "fact_kind") rather than
// silently decoding it away, mirroring
// TestLoadCodeownersFamilyOduRejectsUnknownJSONField exactly (see that test's
// doc comment for the #5994 false-attestation incident this pattern exists
// to prevent).
//
// The well-formed probe carries every real envelope field (collector,
// schema_version, source_system, scope_kind, partition_key, metadata,
// observed_at, trigger_kind) -- the full shape the committed cassette
// carries, not just the load-bearing subset this loader consumes.
func TestLoadSubmodulePinFamilyOduRejectsUnknownJSONField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	goodRaw := `{
  "collector": "git",
  "schema_version": "1",
  "scopes": [
    {
      "scope_id": "scope-1",
      "source_system": "git",
      "scope_kind": "repo",
      "collector_kind": "git",
      "partition_key": "scope-1",
      "metadata": {"fixture": "typo-probe"},
      "generation_id": "gen-1",
      "observed_at": "2026-08-15T00:00:00Z",
      "trigger_kind": "snapshot",
      "facts": [
        {
          "fact_kind": "submodule.pin",
          "schema_version": "1",
          "stable_fact_key": "k1",
          "collector_kind": "git",
          "source_confidence": "high",
          "payload": {"parent_repo_id": "r1", "submodule_path": "vendor/lib"}
        }
      ]
    }
  ]
}`
	goodPath := filepath.Join(dir, "good.json")
	if err := os.WriteFile(goodPath, []byte(goodRaw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadSubmodulePinFamilyOdu(goodPath); err != nil {
		t.Fatalf("loadSubmodulePinFamilyOdu rejected a well-formed cassette carrying every real envelope field: %v", err)
	}

	// Each case is a field the narrow struct never declared, so a permissive
	// decoder drops all of them silently. schema_version is called out
	// separately because an empty version reads as "latest": the projection
	// would accept a fact live replay quarantines.
	for _, tc := range []struct {
		name, from, to string
	}{
		{"fact envelope key", `"fact_kind": "submodule.pin"`, `"fact_knd": "submodule.pin"`},
		{"fact schema_version", `"schema_version": "1",
          "stable_fact_key"`, `"schema_versoin": "1",
          "stable_fact_key"`},
		{"scope key", `"scope_id": "scope-1"`, `"scope_di": "scope-1"`},
		{"top-level key", `"collector": "git"`, `"collectr": "git"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			badRaw := strings.Replace(goodRaw, tc.from, tc.to, 1)
			if badRaw == goodRaw {
				t.Fatalf("test setup bug: %q did not match the probe cassette", tc.from)
			}
			badPath := filepath.Join(t.TempDir(), "typo.json")
			if err := os.WriteFile(badPath, []byte(badRaw), 0o600); err != nil {
				t.Fatalf("write temp cassette: %v", err)
			}
			if _, err := loadSubmodulePinFamilyOdu(badPath); err == nil {
				t.Fatalf("loadSubmodulePinFamilyOdu accepted a cassette with an unknown field (%s typo)", tc.name)
			}
		})
	}
}

// TestLoadSubmodulePinFamilyOduRejectsTrailingContent closes the gap
// switching from json.Unmarshal to json.Decoder would otherwise reopen:
// Decode reads exactly one JSON value and stops, so a second concatenated
// document would be silently ignored where Unmarshal rejected it.
func TestLoadSubmodulePinFamilyOduRejectsTrailingContent(t *testing.T) {
	t.Parallel()

	raw := `{
  "collector": "git",
  "schema_version": "1",
  "scopes": [
    {
      "scope_id": "scope-1",
      "generation_id": "gen-1",
      "facts": [
        {"fact_kind": "submodule.pin", "schema_version": "1", "payload": {"parent_repo_id": "r1", "submodule_path": "vendor/lib"}}
      ]
    }
  ]
}
{"scopes": []}`
	path := filepath.Join(t.TempDir(), "trailing.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadSubmodulePinFamilyOdu(path); err == nil {
		t.Fatal("loadSubmodulePinFamilyOdu accepted a cassette with a second concatenated JSON document")
	}
}

// TestLoadSubmodulePinFamilyOduRejectsWrongScopeCount and
// TestLoadSubmodulePinFamilyOduRejectsEmptyFacts prove the two fail-closed
// checks loadSubmodulePinFamilyOdu performs beyond strict decoding: a
// multi-scope (or zero-scope) cassette would make the expected-edge set
// ambiguous about which scope produced an edge, and a scope with no facts
// would make every downstream assertion vacuous.
func TestLoadSubmodulePinFamilyOduRejectsWrongScopeCount(t *testing.T) {
	t.Parallel()

	raw := `{"collector": "git", "schema_version": "1", "scopes": []}`
	path := filepath.Join(t.TempDir(), "no-scopes.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadSubmodulePinFamilyOdu(path); err == nil {
		t.Fatal("loadSubmodulePinFamilyOdu accepted a cassette with zero scopes")
	}
}

func TestLoadSubmodulePinFamilyOduRejectsEmptyFacts(t *testing.T) {
	t.Parallel()

	raw := `{"collector": "git", "schema_version": "1", "scopes": [{"scope_id": "scope-1", "generation_id": "gen-1", "facts": []}]}`
	path := filepath.Join(t.TempDir(), "no-facts.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadSubmodulePinFamilyOdu(path); err == nil {
		t.Fatal("loadSubmodulePinFamilyOdu accepted a cassette whose only scope carries no facts")
	}
}
