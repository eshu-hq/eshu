// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"fmt"
	"io"
)

// writef writes one formatted fragment to w and returns the write error
// unchanged.
//
// Every renderer in this package writes through here rather than calling
// fmt.Fprintf directly, for one reason: the repo's wrapcheck linter exempts
// go/cmd/* but not go/internal/cli/*, so a bare `return err` from fmt.Fprintf
// fails the lint. Wrapping each of those sites individually would rewrite the
// operator-facing text of a write failure -- the message a broken pipe or a
// full disk produces -- which is exactly the kind of silent output change this
// extraction must not make. Funnelling through one helper keeps the bytes and
// the error text identical and puts the exemption in one reviewable place.
// internal/cli/freshness carries the same helper for the same reason.
//
//nolint:wrapcheck // Deliberate: wrapping here would alter the operator-facing text of every write failure.
func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}
