// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
)

// MinKeyBytes is the shortest key material NewKey accepts. A recording key is
// generated per corpus (`openssl rand -hex 32` yields 64 bytes of hex text)
// and never committed; a short key would make the HMAC pseudonyms guessable
// offline for low-entropy inputs such as account ids.
const MinKeyBytes = 32

// tokenDomain prefixes every HMAC input. The CLASS of a token is deliberately
// not part of the input: two collectors that classify the same raw token
// differently (aws: account_id, oci: ECR host label) must still agree on its
// pseudonym so cross-cassette joins survive. Only the output format carries
// the class.
const tokenDomain = "recordpseudo/v1\x00"

// fingerprintDomain is a separate HMAC domain for Key.Fingerprint so the
// fingerprint can never collide with the pseudonym of a raw token whose text
// happens to be "fingerprint".
const fingerprintDomain = "recordpseudo/v1/fingerprint"

// Key is the recording key: deployment-scoped secret material that keys every
// pseudonym HMAC. The zero Key is unusable; NewKey is the only constructor.
type Key struct {
	material []byte
}

// NewKey validates and copies key material. Surrounding whitespace is
// trimmed; anything shorter than MinKeyBytes after trimming is rejected.
func NewKey(material []byte) (Key, error) {
	trimmed := bytes.TrimSpace(material)
	if len(trimmed) < MinKeyBytes {
		return Key{}, fmt.Errorf("recordpseudo: key material must be at least %d bytes, got %d", MinKeyBytes, len(trimmed))
	}
	copied := make([]byte, len(trimmed))
	copy(copied, trimmed)
	return Key{material: copied}, nil
}

// IsZero reports whether the key carries no material.
func (k Key) IsZero() bool {
	return len(k.material) == 0
}

// Fingerprint is the 8-hex-character identifier of the key that a recording
// stores in cassette.File.PseudonymKeyFingerprint, so a later gate can check
// that sibling cassettes of one corpus were recorded under one key without
// ever seeing the key. The zero Key has an empty fingerprint.
func (k Key) Fingerprint() string {
	if k.IsZero() {
		return ""
	}
	mac := hmac.New(sha256.New, k.material)
	mac.Write([]byte(fingerprintDomain))
	return hex.EncodeToString(mac.Sum(nil))[:8]
}

// mac returns HMAC-SHA256(key, tokenDomain + raw): the class-free pseudonym
// material for one raw token.
func (k Key) mac(raw string) []byte {
	mac := hmac.New(sha256.New, k.material)
	mac.Write([]byte(tokenDomain))
	mac.Write([]byte(raw))
	return mac.Sum(nil)
}

// hexOf returns the first n hex characters of the token's pseudonym material.
func (k Key) hexOf(raw string, n int) string {
	encoded := hex.EncodeToString(k.mac(raw))
	if n > len(encoded) {
		n = len(encoded)
	}
	return encoded[:n]
}

// String renders the key as its fingerprint only, so a %v or %s of a Key,
// or of any struct that carries one, can never print the material.
func (k Key) String() string {
	if k.IsZero() {
		return "recordpseudo.Key(unset)"
	}
	return "recordpseudo.Key(fingerprint=" + k.Fingerprint() + ")"
}

// GoString renders the key for %#v the same way.
func (k Key) GoString() string {
	return k.String()
}

// LogValue renders the key for slog as its fingerprint only.
func (k Key) LogValue() slog.Value {
	return slog.StringValue(k.Fingerprint())
}
