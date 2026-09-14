// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

const testImageRef = "ghcr.io/eshu-hq/demo:1.0.0"

// The production Sealer is the deployment DEK. This assertion is what keeps the
// interface and *secretcrypto.Keyring from drifting apart: cmd/api and
// cmd/mcp-server assign the concrete type into the handler's Sealer field, and
// a signature change there would otherwise only surface at those two call
// sites.
var _ Sealer = (*secretcrypto.Keyring)(nil)

// testKeyring builds a deterministic single-key DEK. id doubles as the key id,
// so two keyrings differ the way a rotation makes them differ (KeyringFromEnv
// fingerprints the key material into the id) unless a test deliberately reuses
// an id.
// testAudience is the authorization audience every token in this file is
// sealed for and opened against, so these tests keep exercising the image_ref,
// version and key-state bindings rather than incidentally failing on the
// audience one. TestCursorDoesNotOpenForAnotherAudience proves the audience
// binding itself.
var testAudience = AudienceOf(querycontract.RepositoryAccessFilter{
	AllowedRepositoryIDs: []string{"repository:r_payments"},
	Allowed:              map[string]struct{}{"repository:r_payments": {}},
})

func testKeyring(t *testing.T, id string, fill byte) *secretcrypto.Keyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill ^ byte(i)
	}
	keyring, err := secretcrypto.NewKeyring(secretcrypto.KeyID(id), map[secretcrypto.KeyID][]byte{secretcrypto.KeyID(id): key})
	if err != nil {
		t.Fatalf("secretcrypto.NewKeyring() error = %v", err)
	}
	return keyring
}

// TestSealedCursorRoundTripsEveryKeyState proves sealing preserves the key
// exactly, including the two states a bare string cannot express: the stored
// empty timestamp that sorts first and the absent property that sorts last.
func TestSealedCursorRoundTripsEveryKeyState(t *testing.T) {
	t.Parallel()

	sealer := testKeyring(t, "k1", 0x11)
	for name, key := range map[string]Key{
		"timestamped":     {At: "1760000000042", UID: "uid-sha256:d42"},
		"empty timestamp": {At: "", UID: "uid-sha256:d00"},
		"null tail":       {NullAt: true, UID: "uid-sha256:dff"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			token, err := EncodeCursor(sealer, testImageRef, testAudience, key)
			if err != nil {
				t.Fatalf("EncodeCursor() error = %v", err)
			}
			if !strings.HasPrefix(token, "ESK1.") {
				t.Fatalf("token = %q, want an ESK1 AEAD envelope", token)
			}
			got, err := DecodeCursor(sealer, token, testImageRef, testAudience)
			if err != nil {
				t.Fatalf("DecodeCursor() error = %v", err)
			}
			if got != key {
				t.Fatalf("round-tripped key = %#v, want %#v", got, key)
			}
		})
	}
}

// TestSealedCursorIsNotReadable proves the token discloses nothing on the wire.
// The plaintext still carries the key -- that is the point of sealing rather
// than MACing: the frontier of a cap-reached page is unreadable, which is what
// turns the residue from "one withheld key per span" into counts only.
func TestSealedCursorIsNotReadable(t *testing.T) {
	t.Parallel()

	token, err := EncodeCursor(testKeyring(t, "k1", 0x11), testImageRef, testAudience, Key{At: "1760000000042", UID: "uid-secret"})
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if strings.Contains(token, "uid-secret") || strings.Contains(token, "1760000000042") {
		t.Fatalf("token = %q leaks its plaintext", token)
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(token); err == nil {
		var payload map[string]any
		if json.Unmarshal(decoded, &payload) == nil {
			t.Fatalf("token decodes to readable JSON %#v", payload)
		}
	}
}

// TestDecodeCursorRefusesEveryUnopenableToken is the #6564 round-3 finding 1
// contract at the cursor boundary: the ONLY tokens a sealing deployment
// accepts are the ones it sealed itself, for this image_ref, under a key it
// still holds.
func TestDecodeCursorRefusesEveryUnopenableToken(t *testing.T) {
	t.Parallel()

	sealer := testKeyring(t, "k1", 0x11)
	key := Key{At: "1760000000042", UID: "uid-sha256:d42"}
	valid, err := EncodeCursor(sealer, testImageRef, testAudience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	foreign, err := EncodeCursor(sealer, "ghcr.io/eshu-hq/other:1.0.0", testAudience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	// An envelope this same DEK sealed for a DIFFERENT feature: a provider
	// secret, a TOTP secret, the bootstrap credential. Each uses its own AAD,
	// so none of them is replayable as a cursor.
	otherFeature, err := sealer.Seal(marshalCursor(CursorVersion, testImageRef, key), []byte("eshu/auth/provider-config/secret\x00pc-1"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	unsealed, err := EncodeCursor(nil, testImageRef, testAudience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}

	for name, token := range map[string]string{
		"forged unsealed v2 token":       unsealed,
		"forged v2 token naming any key": base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"ref":"` + testImageRef + `","at":"1760000000000","uid":"zzz"}`)),
		"sealed for another image_ref":   foreign,
		"sealed for another feature":     otherFeature,
		"sealed under a retired key":     mustEncode(t, testKeyring(t, "k2", 0x22), key),
		"sealed under an impostor key":   mustEncode(t, testKeyring(t, "k1", 0x33), key),
		"truncated envelope":             valid[:len(valid)-4],
		"envelope with a flipped tag":    flipLastByte(valid),
		"garbage":                        "not-a-cursor",
		"empty":                          "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := DecodeCursor(sealer, token, testImageRef, testAudience); err == nil {
				t.Fatalf("DecodeCursor(%q) error = nil, want a refusal", token)
			}
		})
	}
}

// TestCursorDoesNotOpenForAnotherAudience is the audience half of the AAD
// binding (#6564 review finding, cursor.go). A sealed cursor carries no scope,
// grant or principal in its plaintext, so nothing after the envelope opens can
// tell that a token was minted in a different authorization context: only the
// AEAD binding can refuse it. Dropping audience from cursorAAD makes this test
// accept the token.
//
// It matters because the design claims a scoped caller "cannot choose where
// such a span starts". Without this binding an unscoped caller -- which keeps
// the offset parameter and can therefore mint a token at an arbitrary raw
// position -- could hand one over and put a scoped caller at a start it never
// paged to.
func TestCursorDoesNotOpenForAnotherAudience(t *testing.T) {
	t.Parallel()

	sealer := testKeyring(t, "k1", 0x11)
	key := Key{At: "1760000000042", UID: "uid-sha256:d42"}
	other := AudienceOf(querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:r_other"},
		Allowed:              map[string]struct{}{"repository:r_other": {}},
	})
	if other == testAudience {
		t.Fatal("the two fixtures share an audience; this test would pass vacuously")
	}

	token, err := EncodeCursor(sealer, testImageRef, other, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if _, err := DecodeCursor(sealer, token, testImageRef, testAudience); err == nil {
		t.Fatal("DecodeCursor() error = nil; a cursor minted for another grant set must not open")
	}
	// Control: the same token opens for the audience it was sealed for, so the
	// refusal above is the binding and not a broken seal.
	if _, err := DecodeCursor(sealer, token, testImageRef, other); err != nil {
		t.Fatalf("control DecodeCursor() error = %v", err)
	}

	// An unscoped caller is its own audience, and that is the separation the
	// exposure actually turned on.
	unscoped := AudienceOf(querycontract.RepositoryAccessFilter{AllScopes: true})
	if unscoped == testAudience {
		t.Fatal("unscoped and scoped callers share an audience")
	}
	unscopedToken, err := EncodeCursor(sealer, testImageRef, unscoped, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if _, err := DecodeCursor(sealer, unscopedToken, testImageRef, testAudience); err == nil {
		t.Fatal("DecodeCursor() error = nil; a scoped caller must not open an unscoped caller's cursor")
	}
}

// TestAudienceOfIsCanonical proves the derivation cannot refuse a caller its
// own cursor over an accident of encoding: the same grants in a different
// order, or listed twice, must produce the same audience, or a client would see
// its in-flight page 400 at random. Different grants must not.
func TestAudienceOfIsCanonical(t *testing.T) {
	t.Parallel()

	base := AudienceOf(querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:r_a", "repository:r_b"},
		AllowedScopeIDs:      []string{"git-repository-scope:repository:r_c"},
	})
	shuffled := AudienceOf(querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:r_b", "repository:r_a", "repository:r_a"},
		AllowedScopeIDs:      []string{"git-repository-scope:repository:r_c"},
	})
	if base != shuffled {
		t.Fatalf("AudienceOf() = %q for reordered, duplicated grants, want %q", shuffled, base)
	}

	fewer := AudienceOf(querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:r_a"},
	})
	if fewer == base {
		t.Fatal("a smaller grant set produced the same audience; a grant change must invalidate the walk")
	}

	// A repository id and a scope id that spell the same string are different
	// grants, so tagging by kind has to survive the hash.
	asRepository := AudienceOf(querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:r_a"},
	})
	asScope := AudienceOf(querycontract.RepositoryAccessFilter{
		AllowedScopeIDs: []string{"repository:r_a"},
	})
	if asRepository == asScope {
		t.Fatal("a repository grant and a scope grant with the same id collided")
	}
}

// TestCursorAADBindsTheImageRef kills the mutation the plaintext ref check
// would otherwise hide. The plaintext carries ref as belt and braces, so a
// cursor sealed for another image_ref is refused twice over; this test seals a
// plaintext whose ref is CORRECT under an AAD for a different image_ref, so
// only the AEAD binding can refuse it. Dropping image_ref from cursorAAD makes
// this test accept the token.
func TestCursorAADBindsTheImageRef(t *testing.T) {
	t.Parallel()

	sealer := testKeyring(t, "k1", 0x11)
	key := Key{At: "1760000000042", UID: "uid-sha256:d42"}
	token, err := sealer.Seal(marshalCursor(CursorVersion, testImageRef, key), cursorAAD("ghcr.io/eshu-hq/other:1.0.0", testAudience))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err := DecodeCursor(sealer, token, testImageRef, testAudience); err == nil {
		t.Fatal("DecodeCursor() error = nil; the AAD must bind the image_ref, not only the plaintext")
	}
	// Control: the same plaintext under the RIGHT AAD opens, so the refusal
	// above is the binding and not a broken seal.
	control, err := sealer.Seal(marshalCursor(CursorVersion, testImageRef, key), cursorAAD(testImageRef, testAudience))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err := DecodeCursor(sealer, control, testImageRef, testAudience); err != nil {
		t.Fatalf("control DecodeCursor() error = %v", err)
	}
}

// TestDecodeCursorRefusesAMalformedSealedPlaintext covers the checks that can
// only fire on a payload this server itself sealed -- a server bug, not a
// forgery. They are kept because a partially-trusted key is worse than a
// restart from page one.
func TestDecodeCursorRefusesAMalformedSealedPlaintext(t *testing.T) {
	t.Parallel()

	sealer := testKeyring(t, "k1", 0x11)
	for name, plaintext := range map[string]string{
		"wrong version":            `{"v":2,"ref":"` + testImageRef + `","at":"x","uid":"u"}`,
		"foreign image_ref":        `{"v":3,"ref":"ghcr.io/eshu-hq/other:1.0.0","at":"x","uid":"u"}`,
		"empty uid":                `{"v":3,"ref":"` + testImageRef + `","at":"x","uid":""}`,
		"missing uid":              `{"v":3,"ref":"` + testImageRef + `","at":"x"}`,
		"null tail with timestamp": `{"v":3,"ref":"` + testImageRef + `","at":"x","nt":true,"uid":"u"}`,
		"not JSON":                 `nonsense`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			token, err := sealer.Seal([]byte(plaintext), cursorAAD(testImageRef, testAudience))
			if err != nil {
				t.Fatalf("Seal() error = %v", err)
			}
			if _, err := DecodeCursor(sealer, token, testImageRef, testAudience); err == nil {
				t.Fatalf("DecodeCursor() error = nil, want a refusal for %s", name)
			}
		})
	}
}

// TestUnsealedCursorIsOnlyUsableWithoutAKey pins the fallback's boundary. A
// deployment with no DEK issues and accepts the unsealed token so callers with
// nothing withheld from them keep paging; the moment a key exists, that token
// is refused. Neither shape is accepted by the other mode, so an operator who
// mounts the key cannot leave an unsealed token working.
func TestUnsealedCursorIsOnlyUsableWithoutAKey(t *testing.T) {
	t.Parallel()

	key := Key{At: "1760000000042", UID: "uid-sha256:d42"}
	unsealed, err := EncodeCursor(nil, testImageRef, testAudience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	got, err := DecodeCursor(nil, unsealed, testImageRef, testAudience)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want the unsealed token to round-trip without a key", err)
	}
	if got != key {
		t.Fatalf("round-tripped key = %#v, want %#v", got, key)
	}

	sealer := testKeyring(t, "k1", 0x11)
	sealed, err := EncodeCursor(sealer, testImageRef, testAudience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if _, err := DecodeCursor(nil, sealed, testImageRef, testAudience); err == nil {
		t.Fatal("DecodeCursor(nil sealer, sealed token) error = nil, want a refusal")
	}
	if _, err := DecodeCursor(sealer, unsealed, testImageRef, testAudience); err == nil {
		t.Fatal("DecodeCursor(sealer, unsealed token) error = nil, want a refusal")
	}
}

// TestSealedCursorsDifferPerCall proves the envelope carries a fresh nonce, so
// two tokens for the same key are not byte-identical and a caller cannot use
// token equality to test whether two pages stopped at the same row.
func TestSealedCursorsDifferPerCall(t *testing.T) {
	t.Parallel()

	sealer := testKeyring(t, "k1", 0x11)
	key := Key{At: "1760000000042", UID: "uid-sha256:d42"}
	first, err := EncodeCursor(sealer, testImageRef, testAudience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	second, err := EncodeCursor(sealer, testImageRef, testAudience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if first == second {
		t.Fatalf("two seals of the same key are byte-identical: %q", first)
	}
}

func mustEncode(t *testing.T, sealer Sealer, key Key) string {
	t.Helper()
	token, err := EncodeCursor(sealer, testImageRef, testAudience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	return token
}

// flipLastByte corrupts one ciphertext byte so the AEAD tag, rather than the
// envelope's structure, is what refuses the token.
func flipLastByte(token string) string {
	raw := []byte(token)
	last := len(raw) - 1
	if raw[last] == 'A' {
		raw[last] = 'B'
		return string(raw)
	}
	raw[last] = 'A'
	return string(raw)
}
