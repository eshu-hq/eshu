// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"fmt"
	"strings"
)

// maxClaimFailureOperationLength bounds a claim failure-operation token. It
// matches the producer sanitize bound in go/internal/collector, so every
// value the legitimate path emits passes this boundary unchanged.
const maxClaimFailureOperationLength = 64

// ValidateClaimFailureOperation enforces the documented failure-operation
// contract at a claim boundary: blank (unrecorded) or a lowercase
// [a-z0-9_] token of bounded length. Anything else — a URL, a response
// body, a credential — is rejected fail-closed so a bypassing caller can
// never persist it to either store or surface it on a read plane.
//
// This is the enforceable redaction point on the claim path. The sibling
// FailureMessage stays free text by design: provider errors legitimately
// contain URLs and status text, so rejecting message content would break
// real failure reporting. The operation token is the bounded classifier,
// and bounding it keeps secrets out of the durable failure record. It is
// checked both before the client posts a mutation and inside the in-tree
// store validation, so both eshu-side legs refuse the same values.
func ValidateClaimFailureOperation(operation string) error {
	if strings.TrimSpace(operation) == "" {
		if operation != "" {
			return fmt.Errorf("failure_operation must not have surrounding whitespace")
		}
		return nil
	}
	if operation != strings.ToLower(operation) {
		return fmt.Errorf("failure_operation must be lowercase")
	}
	if len(operation) > maxClaimFailureOperationLength {
		return fmt.Errorf("failure_operation must be at most %d characters", maxClaimFailureOperationLength)
	}
	for _, r := range operation {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return fmt.Errorf("failure_operation must be a [a-z0-9_] token: never a URL, response body, or credential")
		}
	}
	return nil
}

// validateMutationFailureOperation rejects a mutation whose failure
// operation would leak sensitive fields into the durable claim record.
func validateMutationFailureOperation(mutation ClaimMutation) error {
	if err := ValidateClaimFailureOperation(mutation.FailureOperation); err != nil {
		return err
	}
	return nil
}
