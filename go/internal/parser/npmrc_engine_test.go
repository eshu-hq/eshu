// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultRegistryAdmitsNpmrcUnderTheMetadataPolicy pins the #8978 binding:
// .npmrc is admitted by exact name at any depth, parsed by its own definition,
// and bound to the metadata content-store policy so no body is stored. A
// binding to store, or to a shared hcl, yaml, or raw_text definition, would
// store the credential lines.
func TestDefaultRegistryAdmitsNpmrcUnderTheMetadataPolicy(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, path := range []string{".npmrc", "dir/.npmrc", "services/api/.npmrc", ".NPMRC"} {
		definition, ok := registry.LookupByPath(path)
		if !ok || definition.ParserKey != "npmrc" || definition.Language != "npmrc" {
			t.Fatalf("LookupByPath(%q) = %#v, %v, want the npmrc definition", path, definition, ok)
		}
		if definition.Admission != AdmissionStructuredFact {
			t.Errorf("%q admission = %q, want %q", path, definition.Admission, AdmissionStructuredFact)
		}
		if got := registry.ContentStoreForPath(path); got.Policy != ContentStoreMetadata || got.Redactor != "" || len(got.CredentialKeys) != 0 {
			t.Errorf("ContentStoreForPath(%q) = %+v, want the bare metadata policy", path, got)
		}
	}
	// Only the exact name binds: a lookalike stays unadmitted, so it can never
	// be stored under a policy meant for .npmrc, and .npmrc is never an
	// extension rule.
	for _, path := range []string{".npmrc.json", ".npmrc.bak", "npmrc", "my.npmrc", ".npmrc/config", ".npmrcs", "global.npmrc"} {
		if definition, ok := registry.LookupByPath(path); ok && definition.ParserKey == "npmrc" {
			t.Errorf("LookupByPath(%q) resolved to the npmrc definition", path)
		}
	}
	if !registry.HasContentStorePolicy() {
		t.Fatal("the default registry binds .npmrc to metadata, so it must report a governed registry")
	}
}

// TestNpmrcDefinitionStaysOffSharedDefinitions guards the #8978 constraint
// that .npmrc is its own exact-name definition: the shared hcl, yaml, and
// raw_text definitions must not carry the name.
func TestNpmrcDefinitionStaysOffSharedDefinitions(t *testing.T) {
	t.Parallel()

	var owners []string
	for _, definition := range DefaultRegistry().Definitions() {
		for _, name := range definition.ExactNames {
			if strings.EqualFold(name, ".npmrc") {
				owners = append(owners, definition.ParserKey)
			}
		}
	}
	if len(owners) != 1 || owners[0] != "npmrc" {
		t.Fatalf(".npmrc is bound by %v, want only the npmrc definition", owners)
	}
}

func TestEngineParsesNpmrcRegistryURLsThroughTheRegistry(t *testing.T) {
	t.Parallel()

	const seed = "EngineSeedCredential0123456789"
	body := "registry=https://deploy:" + seed + "@registry.example.com/npm/?token=" + seed + "\n" +
		"@acme:registry=https://npm.acme.example/repo/\n" +
		"//registry.example.com/:_authToken=" + seed + "\n" +
		"email=" + seed + "@example.com\n"
	dir := t.TempDir()
	path := filepath.Join(dir, ".npmrc")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	// IndexSource asks the parsers to echo the file body; this parser must not.
	payload, err := engine.ParsePath(dir, path, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	got := map[string]string{}
	for _, row := range rows {
		if row["config_kind"] != "registry_url" {
			t.Fatalf("row %v is not a registry_url row", row)
		}
		got[row["name"].(string)] = row["value"].(string)
	}
	want := map[string]string{
		"registry":       "https://registry.example.com/npm/",
		"@acme:registry": "https://npm.acme.example/repo/",
	}
	if len(got) != len(want) || got["registry"] != want["registry"] || got["@acme:registry"] != want["@acme:registry"] {
		t.Fatalf("registry rows = %v, want %v", got, want)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if strings.Contains(string(raw), seed) {
		t.Fatalf("payload holds the seeded credential: %s", raw)
	}
	if _, has := payload["source"]; has {
		t.Fatal("payload carries the file body as source")
	}
}
