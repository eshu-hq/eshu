// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretcrypto_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// envMap returns a getenv function backed by a map, matching the
// func(string) string shape used across the repo (e.g. runtime.ResolveAPIKey).
func envMap(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}

func TestKeyringFromEnvUsesInlineKey(t *testing.T) {
	key := testKey(t, 5)
	getenv := envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY": base64.StdEncoding.EncodeToString(key),
	})

	kr, err := secretcrypto.KeyringFromEnv(getenv)
	if err != nil {
		t.Fatalf("KeyringFromEnv: %v", err)
	}

	envelope, err := kr.Seal([]byte("payload"), []byte("aad"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	plaintext, err := kr.Open(envelope, []byte("aad"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plaintext) != "payload" {
		t.Fatalf("plaintext = %q, want payload", plaintext)
	}
}

func TestKeyringFromEnvFileTakesPrecedenceOverInline(t *testing.T) {
	fileKey := testKey(t, 7)
	inlineKey := testKey(t, 9)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "dek.b64")
	if err := writeTestFile(t, keyPath, base64.StdEncoding.EncodeToString(fileKey)); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	getenv := envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY":      base64.StdEncoding.EncodeToString(inlineKey),
		"ESHU_AUTH_SECRET_ENC_KEY_FILE": keyPath,
	})

	kr, err := secretcrypto.KeyringFromEnv(getenv)
	if err != nil {
		t.Fatalf("KeyringFromEnv: %v", err)
	}

	// Seal under the resolved primary and confirm a keyring built directly
	// from the file key (not the inline key) can open it.
	envelope, err := kr.Seal([]byte("payload"), []byte("aad"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	fileOnly, err := secretcrypto.KeyringFromEnv(envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY_FILE": keyPath,
	}))
	if err != nil {
		t.Fatalf("KeyringFromEnv (file only): %v", err)
	}
	if _, err := fileOnly.Open(envelope, []byte("aad")); err != nil {
		t.Fatalf("file-derived keyring could not open envelope sealed by file-precedence keyring: %v", err)
	}

	inlineOnly, err := secretcrypto.KeyringFromEnv(envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY": base64.StdEncoding.EncodeToString(inlineKey),
	}))
	if err != nil {
		t.Fatalf("KeyringFromEnv (inline only): %v", err)
	}
	if _, err := inlineOnly.Open(envelope, []byte("aad")); err == nil {
		t.Fatalf("inline-derived keyring unexpectedly opened envelope sealed under the file key; file did not take precedence")
	}
}

func TestKeyringFromEnvFailsClosedWhenUnset(t *testing.T) {
	_, err := secretcrypto.KeyringFromEnv(envMap(nil))
	if err == nil {
		t.Fatalf("KeyringFromEnv with no env set succeeded, want error")
	}
	if !errors.Is(err, secretcrypto.ErrKeyNotConfigured) {
		t.Fatalf("KeyringFromEnv error = %v, want ErrKeyNotConfigured", err)
	}
}

func TestKeyringFromEnvFailsClosedOnShortKey(t *testing.T) {
	short := make([]byte, 16) // AES-128 sized, not the required 32 bytes
	getenv := envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY": base64.StdEncoding.EncodeToString(short),
	})

	if _, err := secretcrypto.KeyringFromEnv(getenv); err == nil {
		t.Fatalf("KeyringFromEnv with short key succeeded, want error")
	}
}

func TestKeyringFromEnvFailsClosedOnNonBase64Key(t *testing.T) {
	getenv := envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY": "not-valid-base64!!!",
	})

	if _, err := secretcrypto.KeyringFromEnv(getenv); err == nil {
		t.Fatalf("KeyringFromEnv with non-base64 key succeeded, want error")
	}
}

func TestKeyringFromEnvFailsClosedOnMissingFile(t *testing.T) {
	getenv := envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY_FILE": filepath.Join(t.TempDir(), "does-not-exist"),
	})

	if _, err := secretcrypto.KeyringFromEnv(getenv); err == nil {
		t.Fatalf("KeyringFromEnv with missing key file succeeded, want error")
	}
}

func TestKeyringFromEnvDefaultKeyIDIsFingerprint(t *testing.T) {
	key := testKey(t, 3)
	getenv := envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY": base64.StdEncoding.EncodeToString(key),
	})

	kr, err := secretcrypto.KeyringFromEnv(getenv)
	if err != nil {
		t.Fatalf("KeyringFromEnv: %v", err)
	}

	envelope, err := kr.Seal([]byte("payload"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	sum := sha256.Sum256(key)
	wantID := hex.EncodeToString(sum[:])[:8]

	gotID, err := envelopeKeyID(envelope)
	if err != nil {
		t.Fatalf("envelopeKeyID: %v", err)
	}
	if gotID != wantID {
		t.Fatalf("default key id = %q, want SHA-256 fingerprint prefix %q", gotID, wantID)
	}
}

func TestKeyringFromEnvUsesExplicitKeyID(t *testing.T) {
	key := testKey(t, 4)
	getenv := envMap(map[string]string{
		"ESHU_AUTH_SECRET_ENC_KEY":    base64.StdEncoding.EncodeToString(key),
		"ESHU_AUTH_SECRET_ENC_KEY_ID": "prod-dek-2026",
	})

	kr, err := secretcrypto.KeyringFromEnv(getenv)
	if err != nil {
		t.Fatalf("KeyringFromEnv: %v", err)
	}

	envelope, err := kr.Seal([]byte("payload"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	gotID, err := envelopeKeyID(envelope)
	if err != nil {
		t.Fatalf("envelopeKeyID: %v", err)
	}
	if gotID != "prod-dek-2026" {
		t.Fatalf("key id = %q, want prod-dek-2026", gotID)
	}
}
