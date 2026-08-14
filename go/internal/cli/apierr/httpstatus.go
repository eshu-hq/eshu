// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apierr

import "errors"

// HTTPStatusError is the contract a CLI transport error carries when it came
// back from the Eshu API with an HTTP status. The concrete error type lives in
// go/cmd/eshu, which is package main and therefore unimportable; this
// interface is how the internal/cli packages read the status without naming
// that type.
//
// The method is HTTPStatusCode rather than StatusCode because the concrete
// error already has a StatusCode field, and a method cannot share a name with
// a field on the same type.
//
// Deliberately narrow: the five classification sites in go/cmd/eshu that
// inspect an API error all read the status code and none reads the response
// body, so nothing else belongs here. Widening this interface widens what
// every implementation must promise forever.
type HTTPStatusError interface {
	HTTPStatusCode() int
}

// StatusCode reports the HTTP status carried by err, unwrapping the error
// chain to find it. The second result is false when err is nil or when no
// error in the chain carries a status -- a connection refused before any
// response, for example -- which callers must distinguish from a real status
// of 0.
func StatusCode(err error) (int, bool) {
	var statusErr HTTPStatusError
	if !errors.As(err, &statusErr) {
		return 0, false
	}
	return statusErr.HTTPStatusCode(), true
}
