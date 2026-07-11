// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrContentSubstringIndexesNotReady means an all-repository substring read
// was refused until the exact content trigram indexes finish finalizing.
var ErrContentSubstringIndexesNotReady = errors.New("content substring indexes are not ready")

func contentSubstringIndexReadError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55000" &&
		strings.Contains(pgErr.Message, "content substring indexes are not ready") {
		return ErrContentSubstringIndexesNotReady
	}
	return err
}

func writeContentSubstringIndexUnavailable(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, ErrContentSubstringIndexesNotReady) {
		return false
	}
	WriteError(w, http.StatusServiceUnavailable, err.Error())
	return true
}
