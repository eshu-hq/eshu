// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	// #8978 binds .npmrc to metadata, which needs no marker-arm migration. No
	// other definition may bind a non-store policy, and none may bind marker
	// mode, before #8833's marker-arm migration lands (arbiter, 2026-10-24).
	for _, definition := range DefaultRegistry().Definitions() {
		got := definition.ContentStore
		if definition.ParserKey == "npmrc" {
			if got.Policy != ContentStoreMetadata {
				t.Errorf("npmrc binds policy %q in the default registry, want metadata", got.Policy)
			}
			continue
		}
		if got.Policy != ContentStoreStore {
			t.Errorf("%s binds policy %q in the default registry; only npmrc binds a non-store policy before #8833", definition.ParserKey, got.Policy)
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
		"redact with unknown mode":       {Policy: ContentStoreRedact, Redactor: "bogus", CredentialKeys: []string{"k"}},
		"marker without credential keys": {Policy: ContentStoreRedact, Redactor: RedactorMarker},
		"marker with a blank key":        {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{" "}},
		"metadata with a redactor mode":  {Policy: ContentStoreMetadata, Redactor: RedactorMarker},
		"store with credential keys":     {Policy: ContentStoreStore, CredentialKeys: []string{"k"}},
		"key with a comma":               {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"a,b"}},
		"key with a newline":             {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"a\nb"}},
		"key with a NUL":                 {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"a\x00b"}},
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

func TestHasContentStorePolicyIsTrueOnlyWhenADefinitionBindsOne(t *testing.T) {
	t.Parallel()

	if !DefaultRegistry().HasContentStorePolicy() {
		t.Fatal("the default registry binds .npmrc to metadata (#8978), so it is governed")
	}
	if contentStoreRegistry(t, ContentStore{Policy: ContentStoreStore}).HasContentStorePolicy() {
		t.Fatal("an explicit store policy is not a governed registry")
	}
	if !contentStoreRegistry(t, ContentStore{Policy: ContentStoreMetadata}).HasContentStorePolicy() {
		t.Fatal("a metadata definition makes the registry governed")
	}
}

// legacyFingerprint is Registry.Fingerprint as it was before #8812: key,
// language, extensions, exact names, and prefix names, no policy. A registry
// that binds only store policies must keep hashing exactly like this, so
// binding the first non-store definition (#8833) is the only thing that moves
// the freshness hint, and nobody adds an unconditional policy token by accident.
func legacyFingerprint(definitions []Definition) string {
	h := sha256.New()
	for _, definition := range definitions {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\n",
			definition.ParserKey,
			definition.Language,
			strings.Join(definition.Extensions, ","),
			strings.Join(definition.ExactNames, ","),
			strings.Join(definition.PrefixNames, ","))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestAllStoreRegistryFingerprintEqualsThePreChangeValue(t *testing.T) {
	t.Parallel()

	// The default registry binds .npmrc to metadata (#8978), so its hash moves
	// once. With that one policy reset to store it hashes exactly like the
	// pre-#8812 value, which proves the policy token is the only change.
	def := DefaultRegistry()
	definitions := def.Definitions()
	for i := range definitions {
		definitions[i].ContentStore = ContentStore{}
	}
	allStore, err := NewRegistry(definitions)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if got, want := allStore.Fingerprint(), legacyFingerprint(allStore.Definitions()); got != want {
		t.Fatalf("all-store default registry fingerprint = %s, want the pre-#8812 value %s: a store policy must add nothing to the hash", got, want)
	}
	if def.Fingerprint() == allStore.Fingerprint() {
		t.Fatal("the metadata binding for .npmrc must move the default registry fingerprint")
	}
	custom := contentStoreRegistry(t, ContentStore{Policy: ContentStoreStore})
	if got, want := custom.Fingerprint(), legacyFingerprint(custom.Definitions()); got != want {
		t.Fatalf("all-store registry fingerprint = %s, want the pre-#8812 value %s", got, want)
	}
}

// TestFlippingOneDefinitionMovesTheFingerprint flips one definition at a time
// away from store and checks each flip, and each field of the policy, changes
// the hash.
func TestFlippingOneDefinitionMovesTheFingerprint(t *testing.T) {
	t.Parallel()

	base := contentStoreRegistry(t, ContentStore{}).Fingerprint()
	marker := func(mode RedactorMode, keys ...string) ContentStore {
		return ContentStore{Policy: ContentStoreRedact, Redactor: mode, CredentialKeys: keys}
	}
	flips := map[string]ContentStore{
		"store to metadata":     {Policy: ContentStoreMetadata},
		"store to redact":       marker(RedactorMarker, "npmAuthToken"),
		"redact keys changed":   marker(RedactorMarker, "npmAuthToken", "npmAuthIdent"),
		"redact keys replaced":  marker(RedactorMarker, "password"),
		"redact key reordering": marker(RedactorMarker, "npmAuthIdent", "npmAuthToken"),
	}
	seen := map[string]string{"store": base}
	for name, store := range flips {
		fingerprint := contentStoreRegistry(t, store).Fingerprint()
		if fingerprint == base {
			t.Errorf("%s did not change the fingerprint", name)
		}
		if name == "redact key reordering" {
			// Keys are sorted at construction, so order is not identity.
			if fingerprint != fingerprintWith(t, marker(RedactorMarker, "npmAuthToken", "npmAuthIdent")) {
				t.Errorf("key order changed the fingerprint")
			}
			continue
		}
		if prior, dup := seen[fingerprint]; dup {
			t.Errorf("%s fingerprints like %s", name, prior)
		}
		seen[fingerprint] = name
	}

	// The redactor mode is part of the hash. NewRegistry rejects a mode it does
	// not know, so this checks the renderer directly.
	if fingerprintContentStore(marker(RedactorMarker, "k")) == fingerprintContentStore(marker("bogus", "k")) {
		t.Error("the redactor mode is not part of the fingerprint token")
	}
}

func fingerprintWith(t *testing.T, store ContentStore) string {
	t.Helper()
	return contentStoreRegistry(t, store).Fingerprint()
}
