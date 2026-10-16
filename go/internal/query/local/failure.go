// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import "errors"

// Fixed client-facing messages for local identity route failures (#7674).
// Each failing step answers exactly its constant through
// tracing.WriteServerFailure: 500 with the error recorded on the request span,
// or 499 with only the client-cancel event when the caller canceled. The
// store or generator error never reaches the response body.
const (
	bootstrapCreateFailedMessage    = "failed to create local identity bootstrap"
	invitationCreateFailedMessage   = "failed to create local identity invitation"
	apiAccessKeyCreateFailedMessage = "failed to create local identity api token"
	apiAccessKeyListFailedMessage   = "failed to list api tokens"
	apiAccessKeyRotateFailedMessage = "failed to rotate local identity api token"
	totpEnrollBeginFailedMessage    = "failed to begin totp enrollment"
	totpEnrollConfirmFailedMessage  = "failed to confirm totp enrollment"
	sessionCreateFailedMessage      = "failed to create local identity session"
)

// errGeneratedValueEmpty is the server fault recorded on the request span when
// a generated identifier or code came back empty with no error of its own, so
// tracing.WriteServerFailure still has an error to record.
var errGeneratedValueEmpty = errors.New("local identity generated value is empty")

// generatedValueFailure returns err, or errGeneratedValueEmpty when the
// generator failed without one.
func generatedValueFailure(err error) error {
	if err != nil {
		return err
	}
	return errGeneratedValueEmpty
}
