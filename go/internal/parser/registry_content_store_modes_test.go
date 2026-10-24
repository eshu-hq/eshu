// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"strings"
	"testing"
)

// TestNewRegistryAcceptsSkeletonMode pins the #8833 skeleton mode: an
// open-schema file needs no key list, because the allowlist decides which
// values survive.
func TestNewRegistryAcceptsSkeletonMode(t *testing.T) {
	t.Parallel()

	registry := contentStoreRegistry(t, ContentStore{Policy: ContentStoreRedact, Redactor: RedactorSkeleton})
	got := registry.ContentStoreForPath(".npmrc")
	if got.Policy != ContentStoreRedact || got.Redactor != RedactorSkeleton {
		t.Fatalf("got %+v, want redact/skeleton", got)
	}
	if !registry.HasContentStorePolicy() {
		t.Fatal("a skeleton definition makes the registry governed")
	}
}

// TestNewRegistryAcceptsURLUserinfoKeys pins the userinfo-only rule from the
// #8812 binding comment on #8833: a URL-valued listed key keeps its host and
// loses only its userinfo, so it is a separate list from CredentialKeys.
func TestNewRegistryAcceptsURLUserinfoKeys(t *testing.T) {
	t.Parallel()

	registry := contentStoreRegistry(t, ContentStore{
		Policy:          ContentStoreRedact,
		Redactor:        RedactorMarker,
		CredentialKeys:  []string{"npmAuthToken"},
		URLUserinfoKeys: []string{" httpsProxy ", "npmRegistryServer", "httpsProxy"},
	})
	got := registry.ContentStoreForPath(".npmrc")
	if want := []string{"httpsProxy", "npmRegistryServer"}; strings.Join(got.URLUserinfoKeys, ",") != strings.Join(want, ",") {
		t.Fatalf("URLUserinfoKeys = %v, want trimmed, de-duplicated, sorted %v", got.URLUserinfoKeys, want)
	}
	got.URLUserinfoKeys[0] = "mutated"
	if again := registry.ContentStoreForPath(".npmrc"); again.URLUserinfoKeys[0] != "httpsProxy" {
		t.Fatalf("ContentStoreForPath leaked the registry's URL key slice: %v", again.URLUserinfoKeys)
	}
}

func TestNewRegistryRejectsInvalidModeFields(t *testing.T) {
	t.Parallel()

	cases := map[string]ContentStore{
		"skeleton with credential keys": {Policy: ContentStoreRedact, Redactor: RedactorSkeleton, CredentialKeys: []string{"k"}},
		"skeleton with URL keys":        {Policy: ContentStoreRedact, Redactor: RedactorSkeleton, URLUserinfoKeys: []string{"u"}},
		"metadata with URL keys":        {Policy: ContentStoreMetadata, URLUserinfoKeys: []string{"u"}},
		"store with URL keys":           {Policy: ContentStoreStore, URLUserinfoKeys: []string{"u"}},
		"marker with only URL keys":     {Policy: ContentStoreRedact, Redactor: RedactorMarker, URLUserinfoKeys: []string{"u"}},
		"URL key blank":                 {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"k"}, URLUserinfoKeys: []string{" "}},
		"URL key with a comma":          {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"k"}, URLUserinfoKeys: []string{"a,b"}},
		"URL key with a control rune":   {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"k"}, URLUserinfoKeys: []string{"a\tb"}},
		// A key on both lists is ambiguous: whole-value or userinfo-only.
		"key on both lists": {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"k"}, URLUserinfoKeys: []string{"k"}},
		// Keys match case-insensitively, so case does not separate them.
		"key on both lists by case": {Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"Key"}, URLUserinfoKeys: []string{"kEY"}},
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

// TestFingerprintHashesSkeletonAndURLKeys checks the new mode and the new key
// list each move Registry.Fingerprint, and that a marker definition with no URL
// keys hashes exactly as it did under #8812.
func TestFingerprintHashesSkeletonAndURLKeys(t *testing.T) {
	t.Parallel()

	marker := ContentStore{Policy: ContentStoreRedact, Redactor: RedactorMarker, CredentialKeys: []string{"npmAuthToken"}}
	if got, want := fingerprintContentStore(marker), "\x00content_store=redact:marker:npmAuthToken"; got != want {
		t.Fatalf("marker token = %q, want the #8812 rendering %q", got, want)
	}
	withURL := marker
	withURL.URLUserinfoKeys = []string{"httpsProxy"}
	skeleton := ContentStore{Policy: ContentStoreRedact, Redactor: RedactorSkeleton}
	seen := map[string]string{}
	for name, store := range map[string]ContentStore{
		"store":      {},
		"marker":     marker,
		"marker+url": withURL,
		"skeleton":   skeleton,
		"metadata":   {Policy: ContentStoreMetadata},
	} {
		fingerprint := fingerprintWith(t, store)
		if prior, dup := seen[fingerprint]; dup {
			t.Errorf("%s fingerprints like %s", name, prior)
		}
		seen[fingerprint] = name
	}
	if got := contentStoreLabel(skeleton); got != "redact (skeleton)" {
		t.Errorf("skeleton label = %q, want redact (skeleton)", got)
	}
}
