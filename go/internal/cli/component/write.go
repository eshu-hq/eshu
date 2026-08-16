// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"encoding/json"
	"fmt"
	"io"
)

// writef writes one formatted chunk and returns the write error unchanged.
// Every rendering path in this package goes through it so a broken pipe or a
// full disk surfaces as the writer's own error, with no added prefix: these
// strings are the CLI's stable text output, and go/cmd/eshu prints whatever
// comes back verbatim.
func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err //nolint:wrapcheck // go/cmd/eshu prints this error verbatim; a wrap would change operator-visible text
}

// writeJSON writes v as indented JSON with HTML escaping off, so a component
// id or config path containing <, > or & round-trips as the operator typed
// it. Both the component CLI payload and the API envelope go through it,
// which keeps the two JSON surfaces byte-compatible with the single writers
// they had before this package was extracted from go/cmd/eshu.
//
//nolint:wrapcheck // Same reason as writef: this error is the operator-facing text of a failed write, and wrapping would change it.
func writeJSON(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
