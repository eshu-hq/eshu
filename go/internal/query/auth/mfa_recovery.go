// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"encoding/base64"
)

// MFARecoveryContractsVersion versions the MFA recovery-code issuance
// contract in this file (D13, eshu-hq/eshu#9669). A change to the code count,
// the per-code entropy, or the encoding fails the shape test until this
// version moves with it.
const MFARecoveryContractsVersion = "v0"

// MFARecoveryCodeCount is the number of one-time MFA recovery codes the
// setup wizard's final step generates. Matches the count local identity
// invitations commonly seed; enough for repeated recovery without being
// unwieldy to copy.
const MFARecoveryCodeCount = 10

// MFARecoveryCodeBytes is the raw entropy per generated recovery code.
const MFARecoveryCodeBytes = 20

// GenerateMFARecoveryCodes returns MFARecoveryCodeCount fresh recovery codes
// encoded with base64.RawURLEncoding. readEntropy fills each code's buffer;
// production passes crypto/rand.Read and only tests substitute a double to
// drive the failure step. A deterministic newSecret double must never make
// recovery codes predictable, so issuance takes no secret input at all —
// only this entropy reader.
//
// ANY entropy failure returns nil with the error: a partial batch is never
// handed out as if it were complete (fail-closed — the wizard reports the
// step failed and the operator retries with nothing persisted).
func GenerateMFARecoveryCodes(readEntropy func([]byte) (int, error)) ([]string, error) {
	codes := make([]string, 0, MFARecoveryCodeCount)
	for i := 0; i < MFARecoveryCodeCount; i++ {
		var buf [MFARecoveryCodeBytes]byte
		if _, err := readEntropy(buf[:]); err != nil {
			return nil, err
		}
		codes = append(codes, base64.RawURLEncoding.EncodeToString(buf[:]))
	}
	return codes, nil
}
