// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrContentSubstringIndexesNotReady aliases querycontract's error of the
// same name, which this value moved to (#6060) so a handler-family
// subpackage can compare against the exact same instance with errors.Is.
var ErrContentSubstringIndexesNotReady = querycontract.ErrContentSubstringIndexesNotReady

func contentSubstringIndexReadError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55000" &&
		strings.Contains(pgErr.Message, "content substring indexes are not ready") {
		return ErrContentSubstringIndexesNotReady
	}
	return err
}

func writeContentSubstringIndexUnavailable(w http.ResponseWriter, err error) bool {
	return querycontract.WriteContentSubstringIndexUnavailable(w, err)
}
