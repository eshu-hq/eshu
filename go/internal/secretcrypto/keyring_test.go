// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretcrypto_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

func testKey(t *testing.T, seed byte) []byte {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = seed + byte(i)
	}
	return key
}

func mustKeyring(t *testing.T, primary secretcrypto.KeyID, keys map[secretcrypto.KeyID][]byte) *secretcrypto.Keyring {
	t.Helper()
	kr, err := secretcrypto.NewKeyring(primary, keys)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

func TestSealOpenRoundTrip(t *testing.T) {
	kr := mustKeyring(t, "k1", map[secretcrypto.KeyID][]byte{"k1": testKey(t, 1)})
	aad := []byte("eshu:onetime-admin:v1|tenant-a|workspace-a")

	envelope, err := kr.Seal([]byte("super-secret-password"), aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	plaintext, err := kr.Open(envelope, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plaintext) != "super-secret-password" {
		t.Fatalf("Open plaintext = %q, want %q", plaintext, "super-secret-password")
	}
}

func TestSealEnvelopeShape(t *testing.T) {
	kr := mustKeyring(t, "k1", map[secretcrypto.KeyID][]byte{"k1": testKey(t, 1)})

	envelope, err := kr.Seal([]byte("payload"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	parts := strings.Split(envelope, ".")
	if len(parts) != 4 {
		t.Fatalf("envelope %q has %d dot-separated parts, want 4", envelope, len(parts))
	}
	if parts[0] != "ESK1" {
		t.Fatalf("envelope scheme tag = %q, want ESK1", parts[0])
	}
	if parts[1] != "k1" {
		t.Fatalf("envelope key id = %q, want k1", parts[1])
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	kr := mustKeyring(t, "k1", map[secretcrypto.KeyID][]byte{"k1": testKey(t, 1)})
	aad := []byte("aad")

	envelope, err := kr.Seal([]byte("payload"), aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	parts := strings.Split(envelope, ".")
	// Flip the case of the last ciphertext character to corrupt the tag
	// without changing the envelope's structural shape.
	ct := []byte(parts[3])
	last := ct[len(ct)-1]
	if last >= 'a' && last <= 'z' {
		ct[len(ct)-1] = last - 32
	} else {
		ct[len(ct)-1] = 'A'
	}
	parts[3] = string(ct)
	tampered := strings.Join(parts, ".")
	if tampered == envelope {
		t.Fatalf("tampering did not change envelope; test setup is broken")
	}

	if _, err := kr.Open(tampered, aad); err == nil {
		t.Fatalf("Open(tampered) succeeded, want error")
	} else if err != secretcrypto.ErrDecrypt {
		t.Fatalf("Open(tampered) error = %v, want ErrDecrypt", err)
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	sealer := mustKeyring(t, "k1", map[secretcrypto.KeyID][]byte{"k1": testKey(t, 1)})
	opener := mustKeyring(t, "k2", map[secretcrypto.KeyID][]byte{"k2": testKey(t, 2)})
	aad := []byte("aad")

	envelope, err := sealer.Seal([]byte("payload"), aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := opener.Open(envelope, aad); err != secretcrypto.ErrDecrypt {
		t.Fatalf("Open with unknown key id error = %v, want ErrDecrypt", err)
	}
}

func TestOpenRejectsAADMismatch(t *testing.T) {
	kr := mustKeyring(t, "k1", map[secretcrypto.KeyID][]byte{"k1": testKey(t, 1)})

	envelope, err := kr.Seal([]byte("payload"), []byte("aad-a"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := kr.Open(envelope, []byte("aad-b")); err != secretcrypto.ErrDecrypt {
		t.Fatalf("Open with mismatched aad error = %v, want ErrDecrypt", err)
	}
}

func TestOpenFailsClosedOnTruncationAndMalformed(t *testing.T) {
	kr := mustKeyring(t, "k1", map[secretcrypto.KeyID][]byte{"k1": testKey(t, 1)})
	envelope, err := kr.Seal([]byte("payload"), []byte("aad"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	parts := strings.Split(envelope, ".")

	cases := map[string]string{
		"empty":                 "",
		"wrong_scheme":          "ESK0." + parts[1] + "." + parts[2] + "." + parts[3],
		"too_few_parts":         parts[0] + "." + parts[1] + "." + parts[2],
		"too_many_parts":        envelope + ".extra",
		"empty_key_id":          parts[0] + ".." + parts[2] + "." + parts[3],
		"nonce_not_base64":      parts[0] + "." + parts[1] + ".not-valid-base64!!." + parts[3],
		"ciphertext_not_base64": parts[0] + "." + parts[1] + "." + parts[2] + ".not-valid-base64!!",
		"nonce_truncated":       parts[0] + "." + parts[1] + "." + parts[2][:4] + "." + parts[3],
		"ciphertext_truncated":  parts[0] + "." + parts[1] + "." + parts[2] + "." + parts[3][:4],
		"ciphertext_empty":      parts[0] + "." + parts[1] + "." + parts[2] + ".",
		"unknown_key_id":        parts[0] + ".does-not-exist." + parts[2] + "." + parts[3],
	}

	for name, malformed := range cases {
		t.Run(name, func(t *testing.T) {
			plaintext, err := kr.Open(malformed, []byte("aad"))
			if err != secretcrypto.ErrDecrypt {
				t.Fatalf("Open(%q) error = %v, want ErrDecrypt", malformed, err)
			}
			if plaintext != nil {
				t.Fatalf("Open(%q) returned non-nil plaintext %q on failure", malformed, plaintext)
			}
		})
	}
}

func TestSealNonceUniqueness(t *testing.T) {
	kr := mustKeyring(t, "k1", map[secretcrypto.KeyID][]byte{"k1": testKey(t, 1)})

	const n = 500
	nonces := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		envelope, err := kr.Seal([]byte("payload"), []byte("aad"))
		if err != nil {
			t.Fatalf("Seal[%d]: %v", i, err)
		}
		parts := strings.Split(envelope, ".")
		nonce := parts[2]
		if _, dup := nonces[nonce]; dup {
			t.Fatalf("nonce %q reused at iteration %d", nonce, i)
		}
		nonces[nonce] = struct{}{}
	}
	if len(nonces) != n {
		t.Fatalf("collected %d distinct nonces, want %d", len(nonces), n)
	}
}

func TestRotationOldKeyStillOpens(t *testing.T) {
	k1 := testKey(t, 1)
	k2 := testKey(t, 2)
	aad := []byte("aad")

	original := mustKeyring(t, "k1", map[secretcrypto.KeyID][]byte{"k1": k1})
	envelope, err := original.Seal([]byte("payload"), aad)
	if err != nil {
		t.Fatalf("Seal under k1: %v", err)
	}

	rotated := mustKeyring(t, "k2", map[secretcrypto.KeyID][]byte{
		"k1": k1,
		"k2": k2,
	})

	plaintext, err := rotated.Open(envelope, aad)
	if err != nil {
		t.Fatalf("Open old-key envelope after rotation: %v", err)
	}
	if string(plaintext) != "payload" {
		t.Fatalf("plaintext = %q, want %q", plaintext, "payload")
	}

	// New seals must use the new primary key id.
	newEnvelope, err := rotated.Seal([]byte("payload2"), aad)
	if err != nil {
		t.Fatalf("Seal under rotated keyring: %v", err)
	}
	parts := strings.Split(newEnvelope, ".")
	if parts[1] != "k2" {
		t.Fatalf("post-rotation seal used key id %q, want k2", parts[1])
	}
}

func TestNewKeyringFailsClosed(t *testing.T) {
	valid := testKey(t, 1)
	short := []byte("too-short")

	cases := map[string]struct {
		primary secretcrypto.KeyID
		keys    map[secretcrypto.KeyID][]byte
	}{
		"empty_primary":       {primary: "", keys: map[secretcrypto.KeyID][]byte{"k1": valid}},
		"primary_missing":     {primary: "k2", keys: map[secretcrypto.KeyID][]byte{"k1": valid}},
		"no_keys":             {primary: "k1", keys: map[secretcrypto.KeyID][]byte{}},
		"key_wrong_length":    {primary: "k1", keys: map[secretcrypto.KeyID][]byte{"k1": short}},
		"key_id_contains_dot": {primary: "k.1", keys: map[secretcrypto.KeyID][]byte{"k.1": valid}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := secretcrypto.NewKeyring(tc.primary, tc.keys); err == nil {
				t.Fatalf("NewKeyring(%q, %v) succeeded, want error", tc.primary, tc.keys)
			}
		})
	}
}
