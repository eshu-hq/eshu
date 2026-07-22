// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
)

type graphReadHTTPError struct {
	status  int
	code    ErrorCode
	message string
}

// WriteGraphReadError writes the stable HTTP contract for a bounded graph-read
// availability error. It returns false without touching the response when err
// is not one of the shared Neo4jReader errors.
func WriteGraphReadError(w http.ResponseWriter, r *http.Request, err error, capability string) bool {
	mapped, ok := mapGraphReadHTTPError(err)
	if !ok {
		return false
	}
	WriteErrorEnvelope(w, r, mapped.status, &ErrorEnvelope{
		Code:       mapped.code,
		Message:    mapped.message,
		Capability: capability,
	})
	return true
}

func mapGraphReadHTTPError(err error) (graphReadHTTPError, bool) {
	switch {
	case errors.Is(err, ErrGraphUnavailable):
		return graphReadHTTPError{
			status:  http.StatusServiceUnavailable,
			code:    ErrorCodeBackendUnavailable,
			message: ErrGraphUnavailable.Error(),
		}, true
	case errors.Is(err, ErrGraphReadDeadline):
		return graphReadHTTPError{
			status:  http.StatusGatewayTimeout,
			code:    ErrorCodeBackendTimeout,
			message: ErrGraphReadDeadline.Error(),
		}, true
	default:
		return graphReadHTTPError{}, false
	}
}
