// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"errors"
	"net/http"
)

// callChainLookupError marks a store or graph read that failed while
// resolving call-chain endpoint names. resolveCallChainEntityIDs returns two
// kinds of error: this one, whose text can quote backend detail, and a
// disambiguation error built from the caller's own selectors and the
// candidate entity ids, which is the 400 the caller acts on.
type callChainLookupError struct{ err error }

func (e callChainLookupError) Error() string { return e.err.Error() }

func (e callChainLookupError) Unwrap() error { return e.err }

// writeCallChainEndpointFailure answers a failed call-chain endpoint
// resolution: a lookup failure through writeCodeReadFailure with message, and
// any other error as the 400 disambiguation answer.
func writeCallChainEndpointFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	var lookupErr callChainLookupError
	if errors.As(err, &lookupErr) {
		writeCodeReadFailure(w, r, err, capability, message)
		return
	}
	WriteError(w, http.StatusBadRequest, err.Error())
}
