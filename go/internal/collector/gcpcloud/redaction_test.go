// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("gcp-fixture-redaction-key"))
	if err != nil {
		t.Fatalf("redact.NewKey: %v", err)
	}
	return key
}

func TestFingerprintLabelValuesIsDeterministicAndKeyed(t *testing.T) {
	key := testRedactionKey(t)
	labels := map[string]string{"owner": "alice@example.com", "env": "prod"}
	fingerprint := []string{"owner"}

	first := FingerprintLabelValues(labels, fingerprint, key)
	second := FingerprintLabelValues(labels, fingerprint, key)

	if first["owner"] != second["owner"] {
		t.Fatalf("fingerprint not deterministic: %q vs %q", first["owner"], second["owner"])
	}
	if !strings.HasPrefix(first["owner"], "redacted:hmac-sha256:") {
		t.Fatalf("owner value not redacted: %q", first["owner"])
	}
	if strings.Contains(first["owner"], "alice@example.com") {
		t.Fatalf("raw label value leaked: %q", first["owner"])
	}
	if first["env"] != "prod" {
		t.Fatalf("non-fingerprinted label changed: %q, want prod", first["env"])
	}
}

func TestFingerprintMemberClass(t *testing.T) {
	cases := map[string]string{
		"user:alice@example.com":             "user",
		"group:team@example.com":             "group",
		"serviceAccount:svc@proj.iam":        "serviceAccount",
		"domain:example.com":                 "domain",
		"allUsers":                           "public",
		"allAuthenticatedUsers":              "authenticated",
		"deleted:user:bob@example.com?uid=1": "user",
		"":                                   "unknown",
	}
	for member, want := range cases {
		if got := MemberClass(member); got != want {
			t.Fatalf("MemberClass(%q) = %q, want %q", member, got, want)
		}
	}
}

func TestFingerprintMemberDoesNotLeakIdentity(t *testing.T) {
	key := testRedactionKey(t)
	fp := FingerprintMember("user:alice@example.com", key)
	if strings.Contains(fp, "alice@example.com") {
		t.Fatalf("member fingerprint leaked identity: %q", fp)
	}
	if !strings.HasPrefix(fp, "redacted:hmac-sha256:") {
		t.Fatalf("member fingerprint not a redaction marker: %q", fp)
	}
}
