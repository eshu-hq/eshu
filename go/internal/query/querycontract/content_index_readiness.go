// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"errors"
	"net/http"
)

// ErrContentSubstringIndexesNotReady means an all-repository substring read
// was refused until the exact content trigram indexes finish finalizing.
//
// This moved here from root package query's content_reader_index_readiness.go
// (#6060) so a handler-family subpackage -- currently internal/query/code's
// topic-investigation and structural-inventory routes -- can compare a
// returned error against this exact value with errors.Is, the same as root's
// ContentReader that produces it. Root keeps a plain var alias.
var ErrContentSubstringIndexesNotReady = errors.New("content substring indexes are not ready")

// WriteContentSubstringIndexUnavailable writes the stable 503 contract for
// ErrContentSubstringIndexesNotReady and reports whether it did. It returns
// false without touching the response when err is not that error.
func WriteContentSubstringIndexUnavailable(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, ErrContentSubstringIndexesNotReady) {
		return false
	}
	WriteError(w, http.StatusServiceUnavailable, err.Error())
	return true
}
