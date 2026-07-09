// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package secretcrypto is Eshu's shared at-rest encryption substrate for
// reversible identity secrets: the one-time admin bootstrap credential
// (#4963) and provider-config write-only secrets such as OIDC client
// secrets and SAML signing keys (#4966).
//
// The primitive is AES-256-GCM (stdlib crypto/aes + crypto/cipher). A
// Keyring holds one or more 32-byte data-encryption keys (DEKs) indexed by
// KeyID, with one key designated primary. Seal always encrypts under the
// primary key with a fresh 12-byte crypto/rand nonce and returns a
// self-describing text envelope:
//
//	ESK1.<key_id>.<b64url(nonce)>.<b64url(ciphertext||gcm_tag)>
//
// ESK1 is the scheme and version tag. key_id records which DEK sealed the
// envelope, which is what makes key rotation possible: Open resolves the key
// by the id embedded in the envelope rather than always using the primary,
// so old envelopes keep decrypting after a new primary key is introduced.
// nonce and ciphertext are base64url (no padding) so the envelope is safe to
// store in any TEXT column.
//
// Open fails closed on every failure mode — unknown key_id, tag mismatch,
// truncation, malformed structure, or additional-authenticated-data (AAD)
// mismatch — and returns only the opaque ErrDecrypt. There is no partial
// plaintext, no error that distinguishes one failure mode from another, and
// no cleartext fallback. Callers that need an actionable operator message
// (for example "DEK differs from generation") add that context themselves;
// this package never leaks which check failed.
//
// AAD binds an envelope to the row/slot it seals so a ciphertext copied
// between rows cannot be silently reused (a confused-deputy / cut-and-paste
// attack). AAD is never stored in the envelope; callers reconstruct it
// deterministically from the row's identity (see the AAD schemes documented
// in README.md) and pass the same bytes to both Seal and Open.
//
// KeyringFromEnv sources the primary DEK from the environment
// (ESHU_AUTH_SECRET_ENC_KEY / ESHU_AUTH_SECRET_ENC_KEY_FILE /
// ESHU_AUTH_SECRET_ENC_KEY_ID). It never auto-generates an ephemeral key: an
// ephemeral DEK would make every previously sealed envelope permanently
// undecryptable after a restart, unlike an ephemeral API token. See
// KeyringFromEnv's doc comment and README.md for the fail-closed contract.
//
// This package is a pure library: it has no database, HTTP, or CLI wiring.
// Startup wiring, telemetry, and the DEK-required-vs-optional policy belong
// to its callers (#4963, #4966), not here.
package secretcrypto
