// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"errors"
	"net/http"
)

// Sentinel errors for the bounded graph-read policy. Handlers compare against
// these with errors.Is to decide the HTTP contract below, which makes them part
// of the read contract rather than an implementation detail of the driver.
var (
	// ErrGraphReadDeadline reports that the bounded graph-read budget expired.
	ErrGraphReadDeadline = errors.New("graph query exceeded its deadline")
	// ErrGraphUnavailable reports that the graph backend could not serve a read.
	ErrGraphUnavailable = errors.New("graph temporarily unavailable; retry after graph health is restored")
)

type graphReadHTTPError struct {
	status  int
	code    ErrorCode
	message string
}

// WriteGraphReadError writes the stable HTTP contract for a bounded graph-read
// availability error. It returns false without touching the response when err
// is not one of the shared graph-read errors, leaving the caller's own mapping
// in place.
func WriteGraphReadError(w http.ResponseWriter, r *http.Request, err error, capability string) bool {
	status, errEnv, ok := GraphReadErrorEnvelope(err, capability)
	if !ok {
		return false
	}
	WriteErrorEnvelope(w, r, status, errEnv)
	return true
}

// GraphReadErrorEnvelope returns the same stable status and error envelope that
// WriteGraphReadError would write, for seams that return an envelope to their
// caller instead of writing the response themselves. It reports false when err
// is not one of the shared graph-read errors.
func GraphReadErrorEnvelope(err error, capability string) (int, *ErrorEnvelope, bool) {
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
