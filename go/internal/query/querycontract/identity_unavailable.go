// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"errors"
	"net/http"
)

// ErrIdentityStoreUnavailable reports that a credential could not be evaluated
// because the identity store (PostgreSQL) was unreachable or timed out, so the
// credential was never judged either way (#7586). The auth middleware answers
// it with a retryable 503 and an unavailable audit decision, never a 401: a 401
// reads as a credential problem and is not retryable. Its text is the only
// thing a client may see; the cause stays reachable through errors.Is and
// errors.As for classification and operator logs.
var ErrIdentityStoreUnavailable = errors.New("identity store temporarily unavailable; retry shortly")

// IdentityStoreUnavailableEnvelope returns the 503 backend_unavailable error
// envelope for a credential the identity store could not evaluate. It is marked
// retryable per verdict, like the stale-reader verdict (#7523, #7536), so
// WriteErrorEnvelope adds Retry-After and the envelope details carry the same
// hint for callers that do not see headers.
func IdentityStoreUnavailableEnvelope() *ErrorEnvelope {
	return &ErrorEnvelope{
		Code:      ErrorCodeBackendUnavailable,
		Message:   ErrIdentityStoreUnavailable.Error(),
		Details:   map[string]any{"retry_after_seconds": BackendUnavailableRetryAfterSeconds},
		retryable: true,
	}
}

// WriteIdentityStoreUnavailable writes the retryable 503 for a credential the
// identity store could not evaluate (#7586). It sets no WWW-Authenticate
// challenge: the credential was never judged, so this is not an authentication
// failure.
func WriteIdentityStoreUnavailable(w http.ResponseWriter, r *http.Request) {
	WriteErrorEnvelope(w, r, http.StatusServiceUnavailable, IdentityStoreUnavailableEnvelope())
}
