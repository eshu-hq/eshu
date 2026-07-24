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
	status, errEnv, ok := graphReadErrorEnvelope(err, capability)
	if !ok {
		return false
	}
	WriteErrorEnvelope(w, r, status, errEnv)
	return true
}

// graphReadErrorEnvelope returns the same stable status and error envelope that
// WriteGraphReadError would write, for seams that return an envelope to their
// caller instead of writing the response themselves (for example
// BuildServiceStoryEnvelope). It reports false when err is not one of the
// shared Neo4jReader errors, leaving the caller's existing mapping in place.
func graphReadErrorEnvelope(err error, capability string) (int, *ErrorEnvelope, bool) {
	mapped, ok := mapGraphReadHTTPError(err)
	if !ok {
		return 0, nil, false
	}
	return mapped.status, &ErrorEnvelope{
		Code:       mapped.code,
		Message:    mapped.message,
		Capability: capability,
	}, true
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
