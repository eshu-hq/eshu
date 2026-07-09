// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	// schemeTag is the envelope scheme+version prefix. Bump this if the
	// envelope shape or AEAD primitive ever changes; Open must reject any
	// tag it does not recognize rather than guess a format.
	schemeTag = "ESK1"
	// aes256KeySize is the required raw key length in bytes for AES-256.
	aes256KeySize = 32
	// gcmNonceSize is the standard AES-GCM nonce length in bytes. A fresh
	// nonce is generated for every Seal call; nonces are never derived or
	// reused.
	gcmNonceSize = 12
	// envelopeParts is the number of dot-separated fields a well-formed
	// envelope has: scheme, key_id, nonce, ciphertext.
	envelopeParts = 4
)

// ErrDecrypt is returned by Open for every decryption failure: unknown
// key_id, authentication tag mismatch, truncated or malformed input, or an
// AAD mismatch. It is intentionally opaque — the caller cannot distinguish
// failure modes from the error alone — so Open never becomes an oracle an
// attacker can use to learn which part of a forged envelope was wrong.
var ErrDecrypt = errors.New("secretcrypto: decrypt failed")

// KeyID identifies which data-encryption key (DEK) sealed an envelope. It is
// embedded in the envelope text, which is what makes rotation possible:
// Seal always uses the keyring's primary key, but Open resolves the key by
// whichever id sealed the envelope rather than always trying primary.
//
// A KeyID must not contain '.' — the envelope format uses '.' as the field
// separator, and a KeyID containing one would make the envelope ambiguous
// to parse.
type KeyID string

// Keyring holds one or more 32-byte AES-256 DEKs indexed by KeyID, with one
// key designated primary for new seals.
//
// A Keyring is immutable after construction (NewKeyring copies all key
// material) and is safe for concurrent use.
type Keyring struct {
	primary KeyID
	keys    map[KeyID][]byte
}

// NewKeyring builds a Keyring from a primary key id and the set of DEKs it
// may resolve.
//
// It fails closed on any configuration problem: primary must be non-empty
// and present in keys, keys must be non-empty, every key must be exactly 32
// bytes, and no KeyID may contain '.'. A misconfigured keyring must never
// silently seal or open with the wrong key, so construction rejects
// ambiguous input rather than guessing.
func NewKeyring(primary KeyID, keys map[KeyID][]byte) (*Keyring, error) {
	if primary == "" {
		return nil, fmt.Errorf("secretcrypto: primary key id must not be empty")
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("secretcrypto: keyring must have at least one key")
	}
	if _, ok := keys[primary]; !ok {
		return nil, fmt.Errorf("secretcrypto: primary key id %q not present in keys", primary)
	}

	copied := make(map[KeyID][]byte, len(keys))
	for id, key := range keys {
		if strings.Contains(string(id), ".") {
			return nil, fmt.Errorf("secretcrypto: key id %q must not contain '.'", id)
		}
		if len(key) != aes256KeySize {
			return nil, fmt.Errorf("secretcrypto: key %q must be %d bytes, got %d", id, aes256KeySize, len(key))
		}
		buf := make([]byte, len(key))
		copy(buf, key)
		copied[id] = buf
	}

	return &Keyring{primary: primary, keys: copied}, nil
}

// Seal encrypts plaintext under the keyring's primary key with a fresh
// crypto/rand nonce, binds aad as AEAD additional data, and returns the text
// envelope. aad is not stored in the envelope; Open must be called with the
// identical aad bytes to decrypt successfully.
func (k *Keyring) Seal(plaintext, aad []byte) (string, error) {
	gcm, err := newGCM(k.keys[k.primary])
	if err != nil {
		return "", fmt.Errorf("secretcrypto: build cipher: %w", err)
	}

	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretcrypto: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)

	return fmt.Sprintf(
		"%s.%s.%s.%s",
		schemeTag,
		k.primary,
		base64.RawURLEncoding.EncodeToString(nonce),
		base64.RawURLEncoding.EncodeToString(ciphertext),
	), nil
}

// Open decrypts an envelope previously produced by Seal, verifying it was
// bound to the same aad. It fails closed on every failure mode — unknown
// key_id, authentication tag mismatch, truncated or malformed envelope
// structure, or an aad mismatch — always returning the opaque ErrDecrypt.
// There is no partial plaintext and no cleartext fallback on failure.
func (k *Keyring) Open(envelope string, aad []byte) ([]byte, error) {
	keyID, nonce, ciphertext, err := parseEnvelope(envelope)
	if err != nil {
		return nil, ErrDecrypt
	}

	key, ok := k.keys[keyID]
	if !ok {
		return nil, ErrDecrypt
	}

	gcm, err := newGCM(key)
	if err != nil {
		return nil, ErrDecrypt
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, ErrDecrypt
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// newGCM builds an AES-256-GCM AEAD from a raw key. key must already be
// validated to aes256KeySize by the caller (NewKeyring enforces this for
// every key a Keyring can hold).
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretcrypto: new AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretcrypto: new AES-GCM AEAD: %w", err)
	}
	return gcm, nil
}

// parseEnvelope splits and decodes an ESK1 envelope into its key id, nonce,
// and ciphertext fields without doing any cryptographic work. Every failure
// here is collapsed by the caller (Open) into the opaque ErrDecrypt; this
// function's distinct error messages exist only to aid local debugging, not
// as an external contract.
func parseEnvelope(envelope string) (KeyID, []byte, []byte, error) {
	parts := strings.Split(envelope, ".")
	if len(parts) != envelopeParts {
		return "", nil, nil, fmt.Errorf("secretcrypto: envelope has %d fields, want %d", len(parts), envelopeParts)
	}
	if parts[0] != schemeTag {
		return "", nil, nil, fmt.Errorf("secretcrypto: unsupported envelope scheme %q", parts[0])
	}
	if parts[1] == "" {
		return "", nil, nil, fmt.Errorf("secretcrypto: envelope has empty key id")
	}

	nonce, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", nil, nil, fmt.Errorf("secretcrypto: decode envelope nonce: %w", err)
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", nil, nil, fmt.Errorf("secretcrypto: decode envelope ciphertext: %w", err)
	}
	if len(ciphertext) == 0 {
		return "", nil, nil, fmt.Errorf("secretcrypto: envelope ciphertext is empty")
	}

	return KeyID(parts[1]), nonce, ciphertext, nil
}
