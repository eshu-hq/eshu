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

// wrapperSeed builds a credential-shaped literal by concatenation so no
// secret-looking token sits in the source file (GitHub push protection).
func wrapperSeed(label string) string {
	return "wrapper" + "Seed" + label + "0123456789"
}

// TestDefaultRegistryAdmitsGradleWrapperUnderTheMetadataPolicy pins the #8822
// binding: gradle-wrapper.properties is admitted by exact name at any depth,
// parsed by its own definition, and bound to the metadata content-store policy
// so no body is stored. A binding to the gradle definition would store the
// raw file, and a distributionUrl can carry user:password@ userinfo.
func TestDefaultRegistryAdmitsGradleWrapperUnderTheMetadataPolicy(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, path := range []string{
		"gradle-wrapper.properties",
		"gradle/wrapper/gradle-wrapper.properties",
		"services/api/gradle/wrapper/gradle-wrapper.properties",
		"gradle/wrapper/GRADLE-WRAPPER.PROPERTIES",
	} {
		definition, ok := registry.LookupByPath(path)
		if !ok || definition.ParserKey != "gradle_wrapper" || definition.Language != "gradle_wrapper" {
			t.Fatalf("LookupByPath(%q) = %#v, %v, want the gradle_wrapper definition", path, definition, ok)
		}
		if definition.Admission != AdmissionStructuredFact || definition.Ecosystem != "java" {
			t.Errorf("%q admission = %q ecosystem = %q, want structured_fact and java", path, definition.Admission, definition.Ecosystem)
		}
		if got := registry.ContentStoreForPath(path); got.Policy != ContentStoreMetadata || got.Redactor != "" || len(got.CredentialKeys) != 0 {
			t.Errorf("ContentStoreForPath(%q) = %+v, want the bare metadata policy", path, got)
		}
	}
	// Only the exact name binds. Other .properties files stay unadmitted, and
	// a lookalike never reaches the wrapper definition's metadata policy.
	for _, path := range []string{
		"gradle.properties", "wrapper.properties", "gradle-wrapper.properties.bak",
		"my-gradle-wrapper.properties", "gradle-wrapper.properties/x", "gradle-wrapper.json",
	} {
		if definition, ok := registry.LookupByPath(path); ok && definition.ParserKey == "gradle_wrapper" {
			t.Errorf("LookupByPath(%q) resolved to the gradle_wrapper definition", path)
		}
	}
}

// TestGradleWrapperNameStaysOffTheGradleDefinition guards the credential
// constraint: the gradle build-script definition stores its files, so it must
// never carry the wrapper name.
func TestGradleWrapperNameStaysOffTheGradleDefinition(t *testing.T) {
	t.Parallel()

	var owners []string
	for _, definition := range DefaultRegistry().Definitions() {
		for _, name := range definition.ExactNames {
			if strings.EqualFold(name, "gradle-wrapper.properties") {
				owners = append(owners, definition.ParserKey)
			}
		}
	}
	if len(owners) != 1 || owners[0] != "gradle_wrapper" {
		t.Fatalf("gradle-wrapper.properties is bound by %v, want only the gradle_wrapper definition", owners)
	}
}

func TestEngineParsesGradleWrapperThroughTheRegistry(t *testing.T) {
	t.Parallel()

	user, pass, token, other := wrapperSeed("User"), wrapperSeed("Pass"), wrapperSeed("Query"), wrapperSeed("Other")
	seeds := []string{user, pass, token, other}
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	cases := []struct {
		name        string
		body        string
		wantVersion string
		wantType    string
		wantNoRow   bool
	}{
		{
			name: "userinfo and query in a bin distribution",
			body: "distributionBase=GRADLE_USER_HOME\n" +
				"distributionUrl=https\\://" + user + ":" + pass + "@repo.example.com/gradle/gradle-8.5-bin.zip?token=" + token + "\n" +
				"repoPassword=" + other + "\n",
			wantVersion: "8.5",
			wantType:    "bin",
		},
		{
			name:        "plain all distribution",
			body:        "distributionUrl=https\\://services.gradle.org/distributions/gradle-8.14.3-all.zip\n",
			wantVersion: "8.14.3",
			wantType:    "all",
		},
		{
			name:      "userinfo with an archive name outside the grammar",
			body:      "distributionUrl=https\\://" + user + ":" + pass + "@repo.example.com/dist/custom-build.zip\nrepoPassword=" + other + "\n",
			wantNoRow: true,
		},
		{
			name:      "no distributionUrl",
			body:      "distributionBase=GRADLE_USER_HOME\nrepoPassword=" + other + "\n",
			wantNoRow: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			wrapperDir := filepath.Join(dir, "gradle", "wrapper")
			if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
				t.Fatalf("MkdirAll() error = %v", err)
			}
			path := filepath.Join(wrapperDir, "gradle-wrapper.properties")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			// IndexSource asks the parsers to echo the file body; this one must not.
			payload, err := engine.ParsePath(dir, path, false, Options{IndexSource: true})
			if err != nil {
				t.Fatalf("ParsePath() error = %v", err)
			}
			rows, _ := payload["variables"].([]map[string]any)
			if tc.wantNoRow {
				if len(rows) != 0 {
					t.Fatalf("rows = %v, want none", rows)
				}
			} else {
				if len(rows) != 1 {
					t.Fatalf("rows = %v, want exactly one distribution row", rows)
				}
				row := rows[0]
				if row["value"] != tc.wantVersion || row["distribution_type"] != tc.wantType ||
					row["config_kind"] != "gradle_distribution" || row["name"] != "gradle" {
					t.Fatalf("row = %v, want gradle %s (%s) gradle_distribution", row, tc.wantVersion, tc.wantType)
				}
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			for _, seed := range seeds {
				if strings.Contains(string(raw), seed) {
					t.Fatalf("payload holds the seeded value %q: %s", seed, raw)
				}
			}
			if source, has := payload["source"]; has {
				t.Fatalf("payload carries the file body as source: %v", source)
			}
		})
	}
}
