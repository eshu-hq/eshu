// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretcrypto

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	// keyEnvVar carries the base64-encoded primary DEK inline. #nosec G101 --
	// environment variable name, not a credential.
	keyEnvVar = "ESHU_AUTH_SECRET_ENC_KEY"
	// keyFileEnvVar points at a file holding the base64-encoded primary DEK.
	// When set, it takes precedence over keyEnvVar (mirrors
	// ESHU_AZURE_REDACTION_KEY_FILE's file-over-inline precedence).
	// #nosec G101 -- environment variable name, not a credential.
	keyFileEnvVar = "ESHU_AUTH_SECRET_ENC_KEY_FILE"
	// keyIDEnvVar optionally labels the primary DEK's KeyID. When unset, the
	// KeyID defaults to the first 8 hex characters of SHA-256(key).
	keyIDEnvVar = "ESHU_AUTH_SECRET_ENC_KEY_ID"
	// fingerprintHexLen is how many hex characters of the SHA-256 digest
	// form the default KeyID when ESHU_AUTH_SECRET_ENC_KEY_ID is unset.
	fingerprintHexLen = 8
)

// ErrKeyNotConfigured indicates neither ESHU_AUTH_SECRET_ENC_KEY nor
// ESHU_AUTH_SECRET_ENC_KEY_FILE is set, so KeyringFromEnv has no DEK to
// build a Keyring from.
//
// This package never auto-generates an ephemeral key to paper over that
// gap: whether an absent DEK is fatal depends on the caller's own
// required-vs-optional policy (for example, #4963's bootstrap-credential
// path only requires a DEK when ESHU_AUTH_BOOTSTRAP_MODE=generated or an
// existing provider revision has a sealed secret). Callers that always
// require a DEK should treat this error as fatal; callers with a
// conditional requirement should check errors.Is(err, ErrKeyNotConfigured)
// and apply their own policy.
var ErrKeyNotConfigured = errors.New("secretcrypto: no DEK configured")

// KeyringFromEnv builds a single-primary-key Keyring from
// ESHU_AUTH_SECRET_ENC_KEY, ESHU_AUTH_SECRET_ENC_KEY_FILE, and
// ESHU_AUTH_SECRET_ENC_KEY_ID.
//
// ESHU_AUTH_SECRET_ENC_KEY_FILE takes precedence over
// ESHU_AUTH_SECRET_ENC_KEY when both are set: the file is read and its
// content used, and the inline variable is not consulted at all. The
// resolved value must be standard base64 that decodes to exactly 32 raw
// bytes (AES-256); anything else is a hard error. The KeyID defaults to the
// first 8 hex characters of SHA-256(key) when ESHU_AUTH_SECRET_ENC_KEY_ID is
// unset.
//
// KeyringFromEnv never auto-generates an ephemeral key when no DEK is
// configured (returning ErrKeyNotConfigured instead), unlike
// runtime.ResolveAPIKey (go/internal/runtime/api_key.go:66), which
// auto-generates an ephemeral API token when none is set. That shortcut is
// safe for a bearer token because a fresh one can simply be reissued. It is
// not safe here: an ephemeral DEK would make every envelope sealed before
// the restart permanently undecryptable, since Open has no way to recover a
// key that was never persisted anywhere. Absence of a configured DEK must
// therefore surface as an explicit, fail-closed error for the caller to act
// on, never a silently-working substitute key.
func KeyringFromEnv(getenv func(string) string) (*Keyring, error) {
	raw, err := resolveKeyMaterial(getenv)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, ErrKeyNotConfigured
	}

	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("secretcrypto: decode %s: %w", keyEnvVar, err)
	}
	if len(key) != aes256KeySize {
		return nil, fmt.Errorf("secretcrypto: %s must decode to %d bytes, got %d", keyEnvVar, aes256KeySize, len(key))
	}

	id := strings.TrimSpace(getenv(keyIDEnvVar))
	if id == "" {
		id = fingerprint(key)
	}
	keyID := KeyID(id)

	kr, err := NewKeyring(keyID, map[KeyID][]byte{keyID: key})
	if err != nil {
		return nil, fmt.Errorf("secretcrypto: build keyring from %s: %w", keyIDEnvVar, err)
	}
	return kr, nil
}

// resolveKeyMaterial returns the raw (still base64-encoded, un-decoded) DEK
// text from the file variable when set, else the inline variable. A
// configured-but-unreadable file is a hard error: it never silently falls
// back to the inline variable.
func resolveKeyMaterial(getenv func(string) string) (string, error) {
	if path := strings.TrimSpace(getenv(keyFileEnvVar)); path != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- path is operator-controlled via ESHU_AUTH_SECRET_ENC_KEY_FILE, not request input
		if err != nil {
			return "", fmt.Errorf("secretcrypto: read %s: %w", keyFileEnvVar, err)
		}
		return string(data), nil
	}
	return getenv(keyEnvVar), nil
}

// fingerprint returns the default KeyID for a DEK: the first 8 hex
// characters of SHA-256(key). It is a label for operator visibility and
// rotation bookkeeping, not a secret; the key itself is never derivable from
// it.
func fingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])[:fingerprintHexLen]
}
