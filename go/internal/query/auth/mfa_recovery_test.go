// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

// TestMFARecoveryContractsVersion pins the versioned recovery-code contract
// drained from root package query's setup wizard (D13, #9669).
func TestMFARecoveryContractsVersion(t *testing.T) {
	if MFARecoveryContractsVersion != "v0" {
		t.Fatalf("MFARecoveryContractsVersion = %q, want %q", MFARecoveryContractsVersion, "v0")
	}
}

// TestMFARecoveryCodeShape pins the recovery-code issuance shape: 10 codes,
// each carrying 20 bytes of entropy in base64.RawURLEncoding. Fewer codes or
// less entropy weakens account recovery; changing either is a contract break.
func TestMFARecoveryCodeShape(t *testing.T) {
	if MFARecoveryCodeCount != 10 {
		t.Fatalf("MFARecoveryCodeCount = %d, want 10", MFARecoveryCodeCount)
	}
	if MFARecoveryCodeBytes != 20 {
		t.Fatalf("MFARecoveryCodeBytes = %d, want 20", MFARecoveryCodeBytes)
	}
}

// TestGenerateMFARecoveryCodesFailClosed pins the fail-closed issuance
// behavior: every code decodes to exactly MFARecoveryCodeBytes, the batch is
// unique, and ANY entropy failure returns nil with the error — a partial
// batch is never handed out as if it were complete.
func TestGenerateMFARecoveryCodesFailClosed(t *testing.T) {
	t.Run("production entropy yields full unique batch", func(t *testing.T) {
		codes, err := GenerateMFARecoveryCodes(rand.Read)
		if err != nil {
			t.Fatalf("GenerateMFARecoveryCodes(rand.Read) error = %v", err)
		}
		if len(codes) != MFARecoveryCodeCount {
			t.Fatalf("len(codes) = %d, want %d", len(codes), MFARecoveryCodeCount)
		}
		seen := make(map[string]struct{}, len(codes))
		for i, code := range codes {
			raw, err := base64.RawURLEncoding.DecodeString(code)
			if err != nil {
				t.Fatalf("code %d does not decode: %v", i, err)
			}
			if len(raw) != MFARecoveryCodeBytes {
				t.Fatalf("code %d carries %d entropy bytes, want %d", i, len(raw), MFARecoveryCodeBytes)
			}
			if _, dup := seen[code]; dup {
				t.Fatalf("duplicate recovery code at index %d", i)
			}
			seen[code] = struct{}{}
		}
	})
	t.Run("deterministic entropy keeps exact encoding", func(t *testing.T) {
		zeros := func(b []byte) (int, error) {
			for i := range b {
				b[i] = 0
			}
			return len(b), nil
		}
		codes, err := GenerateMFARecoveryCodes(zeros)
		if err != nil {
			t.Fatalf("GenerateMFARecoveryCodes(zeros) error = %v", err)
		}
		if len(codes) != MFARecoveryCodeCount {
			t.Fatalf("len(codes) = %d, want %d", len(codes), MFARecoveryCodeCount)
		}
		want := base64.RawURLEncoding.EncodeToString(make([]byte, MFARecoveryCodeBytes))
		for i, code := range codes {
			if code != want {
				t.Fatalf("code %d = %q, want %q", i, code, want)
			}
		}
	})
	t.Run("entropy failure returns nil, never partial codes", func(t *testing.T) {
		sentinel := errors.New("entropy unavailable")
		calls := 0
		failing := func(b []byte) (int, error) {
			calls++
			if calls > 2 {
				return 0, sentinel
			}
			return len(b), nil
		}
		codes, err := GenerateMFARecoveryCodes(failing)
		if !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want entropy sentinel", err)
		}
		if codes != nil {
			t.Fatalf("codes = %v on entropy failure: partial batches must never be issued", codes)
		}
	})
}
