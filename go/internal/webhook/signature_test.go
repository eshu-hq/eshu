// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func newSHA256Signature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return sha256SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

func forgeSignature(base string, pos int) string {
	raw := strings.TrimPrefix(base, sha256SignaturePrefix)
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) == 0 {
		return sha256SignaturePrefix + hex.EncodeToString([]byte{0xFF})
	}
	if pos < len(decoded) {
		decoded[pos] ^= 0xFF
	}
	return sha256SignaturePrefix + hex.EncodeToString(decoded)
}

func newPagerDutyV1Signature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestForgedSignatureRejectedAtEveryPosition(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"action":"closed","pull_request":{"merged":true}}`)
	secret := "webhook-secret-key-2026"
	valid := newSHA256Signature(secret, payload)

	tests := []struct {
		name string
		pos  int
	}{
		{name: "first byte forged", pos: 0},
		{name: "middle byte forged", pos: 16},
		{name: "last byte forged", pos: 31},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forged := forgeSignature(valid, tt.pos)
			if forged == valid {
				t.Fatal("forged signature unexpectedly equal to valid")
			}

			if err := VerifyGitHubSignature(payload, secret, forged); err == nil {
				t.Fatal("VerifyGitHubSignature accepted forged signature")
			}
			if err := VerifyBitbucketSignature(payload, secret, forged); err == nil {
				t.Fatal("VerifyBitbucketSignature accepted forged signature")
			}
			if err := VerifyJiraSignature(payload, secret, forged); err == nil {
				t.Fatal("VerifyJiraSignature accepted forged signature")
			}
		})
	}
}

func TestForgedPagerDutySignatureRejectedAtEveryPosition(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"event":{"id":"evt-test","event_type":"incident.triggered","occurred_at":"2026-06-01T12:00:00Z","data":{}}}`)
	secret := "pd-secret-key-2026"
	valid := newPagerDutyV1Signature(secret, payload)
	raw := hex.EncodeToString(hmacSHA256(secret, payload))

	tests := []struct {
		name string
		pos  int
	}{
		{name: "first byte forged", pos: 0},
		{name: "middle byte forged", pos: 16},
		{name: "last byte forged", pos: 31},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forged := make([]byte, 32)
			decoded, _ := hex.DecodeString(raw)
			copy(forged, decoded)
			forged[tt.pos] ^= 0xFF

			forgedSig := "v1=" + hex.EncodeToString(forged)
			if forgedSig == valid {
				t.Fatal("forged signature unexpectedly equal to valid")
			}

			if err := VerifyPagerDutySignature(payload, secret, forgedSig); err == nil {
				t.Fatal("VerifyPagerDutySignature accepted forged signature")
			}
		})
	}
}

func hmacSHA256(secret string, payload []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func TestForgedGitLabTokenRejectedAtEveryPosition(t *testing.T) {
	t.Parallel()

	secret := "gl-token-2026"

	tests := []struct {
		name  string
		token string
	}{
		{name: "first byte forged", token: "Xl-token-2026"},
		{name: "last byte forged", token: "gl-token-202X"},
		{name: "wrong length longer", token: "gl-token-2026-extra"},
		{name: "wrong length shorter", token: "gl-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := VerifyGitLabToken(secret, tt.token); err == nil {
				t.Fatal("VerifyGitLabToken accepted forged token")
			}
		})
	}
}

func TestSignatureVerificationRejectsWrongPrefix(t *testing.T) {
	t.Parallel()

	payload := []byte("test")
	secret := "secret"
	wrongPrefixes := []string{
		"sha1=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha384=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha512=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"",
		"sha256",
		"md5=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	for _, prefix := range wrongPrefixes {
		prefix := prefix
		t.Run("github/"+prefix, func(t *testing.T) {
			t.Parallel()
			if err := VerifyGitHubSignature(payload, secret, prefix); err == nil {
				t.Fatal("VerifyGitHubSignature accepted wrong prefix")
			}
		})
		t.Run("bitbucket/"+prefix, func(t *testing.T) {
			t.Parallel()
			if err := VerifyBitbucketSignature(payload, secret, prefix); err == nil {
				t.Fatal("VerifyBitbucketSignature accepted wrong prefix")
			}
		})
	}
}

func TestSignatureVerificationRejectsHexDecodeErrors(t *testing.T) {
	t.Parallel()

	payload := []byte("test")
	secret := "secret"
	invalidHex := []string{
		"sha256=xyz",
		"sha256=gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg",
		"sha256=ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ",
	}

	for _, sig := range invalidHex {
		sig := sig
		t.Run(sig, func(t *testing.T) {
			t.Parallel()
			if err := VerifyGitHubSignature(payload, secret, sig); err == nil {
				t.Fatal("VerifyGitHubSignature accepted invalid hex")
			}
			if err := VerifyBitbucketSignature(payload, secret, sig); err == nil {
				t.Fatal("VerifyBitbucketSignature accepted invalid hex")
			}
			if err := VerifyJiraSignature(payload, secret, sig); err == nil {
				t.Fatal("VerifyJiraSignature accepted invalid hex")
			}
		})
	}
}

func TestSignatureVerificationRejectsTruncatedSignatures(t *testing.T) {
	t.Parallel()

	payload := []byte("test")
	secret := "secret"
	truncated := []string{
		"sha256=a",
		"sha256=aa",
		"sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	for _, sig := range truncated {
		sig := sig
		t.Run(sig, func(t *testing.T) {
			t.Parallel()
			if err := VerifyGitHubSignature(payload, secret, sig); err == nil {
				t.Fatal("VerifyGitHubSignature accepted truncated signature")
			}
		})
	}
}

func TestSignatureVerificationRejectsEmptySecret(t *testing.T) {
	t.Parallel()

	payload := []byte("test")
	valid := newSHA256Signature("secret", payload)

	if err := VerifyGitHubSignature(payload, "", valid); err == nil {
		t.Fatal("VerifyGitHubSignature accepted empty secret")
	}
	if err := VerifyBitbucketSignature(payload, "", valid); err == nil {
		t.Fatal("VerifyBitbucketSignature accepted empty secret")
	}
	if err := VerifyJiraSignature(payload, "", valid); err == nil {
		t.Fatal("VerifyJiraSignature accepted empty secret")
	}
	if err := VerifyPagerDutySignature(payload, "", newPagerDutyV1Signature("secret", payload)); err == nil {
		t.Fatal("VerifyPagerDutySignature accepted empty secret")
	}
	if err := VerifyGitLabToken("", "token"); err == nil {
		t.Fatal("VerifyGitLabToken accepted empty secret")
	}
}

func TestSignatureVerificationAcceptsValidSignatures(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"ref":"refs/heads/main","repository":{"id":1,"full_name":"org/repo","default_branch":"main"}}`)
	secret := "correct-horse-battery-staple"

	valid := newSHA256Signature(secret, payload)
	if err := VerifyGitHubSignature(payload, secret, valid); err != nil {
		t.Fatalf("VerifyGitHubSignature rejected valid: %v", err)
	}
	if err := VerifyBitbucketSignature(payload, secret, valid); err != nil {
		t.Fatalf("VerifyBitbucketSignature rejected valid: %v", err)
	}
	if err := VerifyJiraSignature(payload, secret, valid); err != nil {
		t.Fatalf("VerifyJiraSignature rejected valid: %v", err)
	}

	pdPayload := []byte(`{"event":{"id":"evt-1","event_type":"incident.triggered","occurred_at":"2026-01-01T00:00:00Z","data":{}}}`)
	pdSig := newPagerDutyV1Signature(secret, pdPayload)
	if err := VerifyPagerDutySignature(pdPayload, secret, pdSig); err != nil {
		t.Fatalf("VerifyPagerDutySignature rejected valid: %v", err)
	}

	if err := VerifyGitLabToken(secret, secret); err != nil {
		t.Fatalf("VerifyGitLabToken rejected valid: %v", err)
	}
}

func TestPagerDutySignatureRejectsNonV1Candidates(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"event":{"id":"evt-test"}}`)
	secret := "secret"

	nonV1 := "v2=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := VerifyPagerDutySignature(payload, secret, nonV1); err == nil {
		t.Fatal("VerifyPagerDutySignature accepted non-v1 signature")
	}

	nonV1WithV1Prefix := "v2=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,v1=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := VerifyPagerDutySignature(payload, secret, nonV1WithV1Prefix); err == nil {
		t.Fatal("VerifyPagerDutySignature accepted non-v1 prefix with bad v1")
	}

	valid := newPagerDutyV1Signature(secret, payload)
	multiWithValid := "v2=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc," + valid
	if err := VerifyPagerDutySignature(payload, secret, multiWithValid); err != nil {
		t.Fatalf("VerifyPagerDutySignature rejected comma-separated valid: %v", err)
	}
}

func TestPagerDutySignatureRejectsInvalidHexInCandidates(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"event":{"id":"evt-test"}}`)
	secret := "secret"
	invalidHex := "v1=gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"
	valid := newPagerDutyV1Signature(secret, payload)
	combo := invalidHex + "," + valid

	if err := VerifyPagerDutySignature(payload, secret, combo); err != nil {
		t.Fatalf("VerifyPagerDutySignature rejected combo with valid: %v", err)
	}
}
