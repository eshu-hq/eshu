// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"strings"
	"testing"
)

func contentStoreRegistry(t *testing.T, store ContentStore) Registry {
	t.Helper()
	registry, err := NewRegistry([]Definition{
		{ParserKey: "go", Language: "go", Extensions: []string{".go"}},
		{ParserKey: "npmrc", Language: "ini", ExactNames: []string{".npmrc"}, ContentStore: store},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

func TestContentStoreForPathDefaultsToStore(t *testing.T) {
	t.Parallel()

	registry := contentStoreRegistry(t, ContentStore{})
	for _, path := range []string{"main.go", "sub/.npmrc", "unregistered.zzz"} {
		if got := registry.ContentStoreForPath(path).Policy; got != ContentStoreStore {
			t.Errorf("ContentStoreForPath(%q).Policy = %q, want %q", path, got, ContentStoreStore)
		}
	}
	for _, definition := range DefaultRegistry().Definitions() {
		if got := definition.ContentStore.Policy; got != ContentStoreStore {
			t.Errorf("%s binds policy %q in the default registry; no production definition binds a non-store policy before #8833", definition.ParserKey, got)
		}
	}
}

func TestContentStoreForPathResolvesMetadataAndRedact(t *testing.T) {
	t.Parallel()

	meta := contentStoreRegistry(t, ContentStore{Policy: ContentStoreMetadata})
	if got := meta.ContentStoreForPath("pkg/.npmrc").Policy; got != ContentStoreMetadata {
		t.Fatalf("policy = %q, want metadata", got)
	}
	if got := meta.ContentStoreForPath("main.go").Policy; got != ContentStoreStore {
		t.Fatalf("unrelated path policy = %q, want store", got)
	}

	redact := contentStoreRegistry(t, ContentStore{
		Policy:         ContentStoreRedact,
		Redactor:       RedactorMarker,
		CredentialKeys: []string{"npmAuthToken", "npmAuthIdent"},
	})
	got := redact.ContentStoreForPath(".npmrc")
	if got.Policy != ContentStoreRedact || got.Redactor != RedactorMarker {
		t.Fatalf("got %+v, want redact/marker", got)
	}
	got.CredentialKeys[0] = "mutated"
	if again := redact.ContentStoreForPath(".npmrc"); again.CredentialKeys[0] != "npmAuthIdent" {
		t.Fatalf("ContentStoreForPath leaked the registry's key slice: %v", again.CredentialKeys)
	}
}

func TestNewRegistryRejectsInvalidContentStore(t *testing.T) {
	t.Parallel()

	cases := map[string]ContentStore{
		"unknown policy":                 {Policy: "drop"},
		"redact without a redactor mode": {Policy: ContentStoreRedact, CredentialKeys: []string{"k"}},
		"redact with unknown mode":       {Policy: ContentStoreRedact, Redactor: "skeleton", CredentialKeys: []string{"k"}},
		"marker without credential keys": {Policy: ContentStoreRedact, Redactor: RedactorMarker},
		"marker with a blank key":        {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{" "}},
		"metadata with a redactor mode":  {Policy: ContentStoreMetadata, Redactor: RedactorMarker},
		"store with credential keys":     {Policy: ContentStoreStore, CredentialKeys: []string{"k"}},
		"metadata with credential keys":  {Policy: ContentStoreMetadata, CredentialKeys: []string{"k"}},
	}
	for name, store := range cases {
		_, err := NewRegistry([]Definition{
			{ParserKey: "npmrc", Language: "ini", ExactNames: []string{".npmrc"}, ContentStore: store},
		})
		if err == nil {
			t.Errorf("%s: NewRegistry accepted %+v", name, store)
			continue
		}
		if !strings.Contains(err.Error(), "npmrc") {
			t.Errorf("%s: error %q does not name the parser key", name, err)
		}
	}
}

// TestRegistryFingerprintIncludesContentStorePolicy pins the #8812 closure: a
// policy change on one definition moves Registry.Fingerprint, so the freshness
// hint moves and every repository re-collects once under the new policy.
func TestRegistryFingerprintIncludesContentStorePolicy(t *testing.T) {
	t.Parallel()

	redact := func(keys ...string) ContentStore {
		return ContentStore{Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: keys}
	}
	store := contentStoreRegistry(t, ContentStore{}).Fingerprint()
	if explicit := contentStoreRegistry(t, ContentStore{Policy: ContentStoreStore}).Fingerprint(); explicit != store {
		t.Fatal("an explicit store policy must hash like the zero value")
	}
	variants := map[string]string{
		"metadata":          contentStoreRegistry(t, ContentStore{Policy: ContentStoreMetadata}).Fingerprint(),
		"redact one key":    contentStoreRegistry(t, redact("npmAuthToken")).Fingerprint(),
		"redact two keys":   contentStoreRegistry(t, redact("npmAuthToken", "npmAuthIdent")).Fingerprint(),
		"redact other keys": contentStoreRegistry(t, redact("password")).Fingerprint(),
	}
	seen := map[string]string{"store": store}
	for name, fingerprint := range variants {
		if prior, dup := seen[fingerprint]; dup {
			t.Errorf("%s fingerprints like %s: the policy is not fully hashed", name, prior)
		}
		seen[fingerprint] = name
	}
}
