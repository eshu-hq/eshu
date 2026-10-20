// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearchserverless

import "testing"

func TestMatchEncryptionKeyPrecedence(t *testing.T) {
	exact := "arn:aws:kms:us-east-1:123456789012:key/exact"
	prefix := "arn:aws:kms:us-east-1:123456789012:key/prefix"
	longer := "arn:aws:kms:us-east-1:123456789012:key/longer"
	bindings := []EncryptionKeyBinding{
		{PolicyName: "short", KMSKeyARN: prefix, CollectionPatterns: []string{"log*"}},
		{PolicyName: "longer", KMSKeyARN: longer, CollectionPatterns: []string{"logspeci*"}},
		{PolicyName: "exact", KMSKeyARN: exact, CollectionPatterns: []string{"logspecial"}},
	}

	tests := []struct {
		name       string
		collection string
		wantKey    string
		wantPolicy string
	}{
		{name: "exact beats prefixes", collection: "logspecial", wantKey: exact, wantPolicy: "exact"},
		{name: "longer prefix beats shorter", collection: "logspecified", wantKey: longer, wantPolicy: "longer"},
		{name: "short prefix only", collection: "logbook", wantKey: prefix, wantPolicy: "short"},
		{name: "no match", collection: "metrics", wantKey: "", wantPolicy: ""},
		{name: "empty name", collection: "", wantKey: "", wantPolicy: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotKey, gotPolicy := matchEncryptionKey(bindings, tc.collection)
			if gotKey != tc.wantKey {
				t.Fatalf("key = %q, want %q", gotKey, tc.wantKey)
			}
			if gotPolicy != tc.wantPolicy {
				t.Fatalf("policy = %q, want %q", gotPolicy, tc.wantPolicy)
			}
		})
	}
}

func TestMatchEncryptionKeyIgnoresAWSOwnedKey(t *testing.T) {
	bindings := []EncryptionKeyBinding{{
		PolicyName:         "owned",
		KMSKeyARN:          "",
		CollectionPatterns: []string{"orders"},
	}}
	if key, _ := matchEncryptionKey(bindings, "orders"); key != "" {
		t.Fatalf("AWS-owned-key binding returned key %q, want empty", key)
	}
}

func TestCollectionPatternFromResource(t *testing.T) {
	tests := map[string]string{
		"collection/orders":  "orders",
		"collection/log*":    "log*",
		"collection/":        "",
		"index/orders/*":     "",
		"":                   "",
		" collection/spaced": "spaced",
	}
	for input, want := range tests {
		if got := CollectionPatternFromResource(input); got != want {
			t.Errorf("CollectionPatternFromResource(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSecurityPolicyResourceIDTypeQualified(t *testing.T) {
	enc := securityPolicyResourceID(SecurityPolicy{Name: "shared", Type: "encryption"})
	net := securityPolicyResourceID(SecurityPolicy{Name: "shared", Type: "network"})
	if enc == net {
		t.Fatalf("encryption and network policies with the same name must get distinct ids: %q == %q", enc, net)
	}
	if got := securityPolicyResourceID(SecurityPolicy{Name: ""}); got != "" {
		t.Fatalf("empty-named policy id = %q, want empty", got)
	}
}
