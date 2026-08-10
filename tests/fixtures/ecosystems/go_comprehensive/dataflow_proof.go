// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package comprehensive

import (
	"database/sql"
	"net/http"
)

// GoldenDataflowHandler exposes bounded CFG, dependence, and taint evidence.
func GoldenDataflowHandler(r *http.Request, db *sql.DB) {
	query := r.FormValue("q")
	if query != "" {
		db.Query(query) //nolint:errcheck // Intentional unsafe sink for the public proof corpus.
	}
}

// GoldenSafeQuery is the adjacent constant-query negative control.
func GoldenSafeQuery(db *sql.DB) {
	db.Query("SELECT 1") //nolint:errcheck // Constant input must never produce taint evidence.
}
