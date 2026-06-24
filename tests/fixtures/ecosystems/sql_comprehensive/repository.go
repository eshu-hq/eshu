// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sqlfixture

import "database/sql"

func listUsers(db *sql.DB) error {
	_, err := db.Exec("SELECT id FROM public.users")
	return err
}
