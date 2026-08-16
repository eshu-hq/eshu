// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadCodeownersFamilyOduRejectsUnknownJSONField proves the cassette
// decoder catches a typo (e.g. "fact_knd" instead of "fact_kind") rather than
// silently decoding it away -- the same false-attestation shape #5994 forced a
// coverage-row withdrawal for: an unnoticed typo here would silently project a
// WRONG fact set from a cassette, and the resulting Odù would still drive an
// exact-set gate whose green result would then attest to something the
// cassette did not actually say.
//
// This family's loader was the last of the three to be hardened. Its doc
// comment claimed parity with codeCallFamilyCassetteFile while still using a
// plain json.Unmarshal against an intentionally narrow struct, so every
// envelope typo outside that struct decoded to a zero value. The two siblings
// gained their strict decoders on #6137, the branch this one is built on.
//
// The well-formed probe carries every real envelope field (collector,
// schema_version, source_system, scope_kind, partition_key, metadata,
// observed_at, trigger_kind) -- the full shape the committed cassette carries,
// not just the load-bearing subset this loader consumes. That half is
// load-bearing in the opposite direction: a strict decoder is only safe if the
// struct declares everything the real cassette holds, or the gate rejects its
// own committed data.
func TestLoadCodeownersFamilyOduRejectsUnknownJSONField(t *testing.T) {
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
          "fact_kind": "codeowners.ownership",
          "schema_version": "1",
          "stable_fact_key": "k1",
          "collector_kind": "git",
          "source_confidence": "high",
          "payload": {"repo_id": "r1"}
        }
      ]
    }
  ]
}`
	goodPath := filepath.Join(dir, "good.json")
	if err := os.WriteFile(goodPath, []byte(goodRaw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadCodeownersFamilyOdu(goodPath); err != nil {
		t.Fatalf("loadCodeownersFamilyOdu rejected a well-formed cassette carrying every real envelope field: %v", err)
	}

	// Each case is a field the narrow struct never declared, so a permissive
	// decoder drops all of them silently. schema_version is called out
	// separately because an empty version reads as "latest": the projection
	// would accept a fact live replay quarantines.
	for _, tc := range []struct {
		name, from, to string
	}{
		{"fact envelope key", `"fact_kind": "codeowners.ownership"`, `"fact_knd": "codeowners.ownership"`},
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
			if _, err := loadCodeownersFamilyOdu(badPath); err == nil {
				t.Fatalf("loadCodeownersFamilyOdu accepted a cassette with an unknown field (%s typo)", tc.name)
			}
		})
	}
}

// TestLoadCodeownersFamilyOduRejectsTrailingContent closes the gap switching
// from json.Unmarshal to json.Decoder would otherwise reopen: Decode reads
// exactly one JSON value and stops, so a second concatenated document would be
// silently ignored where Unmarshal rejected it.
func TestLoadCodeownersFamilyOduRejectsTrailingContent(t *testing.T) {
	t.Parallel()

	raw := `{
  "collector": "git",
  "schema_version": "1",
  "scopes": [
    {
      "scope_id": "scope-1",
      "generation_id": "gen-1",
      "facts": [
        {"fact_kind": "codeowners.ownership", "schema_version": "1", "payload": {"repo_id": "r1"}}
      ]
    }
  ]
}
{"scopes": []}`
	path := filepath.Join(t.TempDir(), "trailing.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadCodeownersFamilyOdu(path); err == nil {
		t.Fatal("loadCodeownersFamilyOdu accepted a cassette with a second concatenated JSON document")
	}
}
