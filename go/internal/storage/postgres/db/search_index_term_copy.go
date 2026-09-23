// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"fmt"
	"strings"
)

// SearchIndexTermCopyUnsupportedError reports that the driver behind a
// Queryer/Executor cannot perform the PostgreSQL COPY protocol InstrumentedDB
// needs for bulk search-index term loads. Driver names the concrete driver
// type when known.
type SearchIndexTermCopyUnsupportedError struct {
	Driver string
}

// Error implements error.
func (e SearchIndexTermCopyUnsupportedError) Error() string {
	if strings.TrimSpace(e.Driver) == "" {
		return "search-index term copy is unsupported by this database"
	}
	return fmt.Sprintf("search-index term copy is unsupported by %s", e.Driver)
}

// UnsupportedSearchIndexTermCopy marks this error as the unsupported-copy
// sentinel that callers detect with errors.As against a minimal interface,
// without importing this concrete type.
func (e SearchIndexTermCopyUnsupportedError) UnsupportedSearchIndexTermCopy() bool {
	return true
}
