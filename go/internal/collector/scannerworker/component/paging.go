// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultStdoutLimitBytes mirrors the host's bounded stdout limit
	// (extensionhost defaultStdoutLimitBytes). Pages sized against it always
	// fit the host's stdout write with room to spare.
	DefaultStdoutLimitBytes = int64(4 * 1024 * 1024)
	// pageEnvelopeHeadroomBytes mirrors collector.pageEnvelopeHeadroomBytes
	// in sdk/go/collector: wire space for the repeated claim and
	// generation, the page envelope, statuses, and JSON framing. The value
	// must stay identical; the bound test pins it.
	pageEnvelopeHeadroomBytes = int64(64 * 1024)
	// maxPagesPerClaim bounds how many calls one claim may reassemble,
	// mirroring the host's DefaultMaxResultPages. A hostile 93 MB result
	// needs about 24 pages at the default bound; exceeding this fails the
	// claim terminal instead of holding it past its deadline one page at a
	// time.
	maxPagesPerClaim = 64
)

// pageBoundForStdout derives the per-call page byte bound from the host's
// stdout limit, mirroring collector.PageBoundForStdout in sdk/go/collector
// until the eshu-sdk/collector pin carries pagination. Reconcile the two
// when the pin advances.
func pageBoundForStdout(stdoutLimitBytes int64) (int64, error) {
	if stdoutLimitBytes <= 0 {
		return 0, fmt.Errorf("stdout limit must be positive")
	}
	bound := stdoutLimitBytes - pageEnvelopeHeadroomBytes
	if bound <= 0 {
		return 0, fmt.Errorf("stdout limit %d leaves no page bytes after %d headroom", stdoutLimitBytes, pageEnvelopeHeadroomBytes)
	}
	return bound, nil
}

// formatCursor mints the opaque resume token for one page index. The host
// echoes it back verbatim on the continuation call; only this component
// interprets it.
func formatCursor(pageIndex int) string {
	return "page-" + strconv.Itoa(pageIndex)
}

// parseCursorIndex resolves a continuation cursor to the page index it
// names, without range context. Format forgeries fail here, before any
// analysis runs.
func parseCursorIndex(cursor string) (int, error) {
	trimmed := strings.TrimSpace(cursor)
	if !strings.HasPrefix(trimmed, "page-") {
		return 0, fmt.Errorf("unknown result cursor %q", trimmed)
	}
	index, err := strconv.Atoi(strings.TrimPrefix(trimmed, "page-"))
	if err != nil || index < 0 {
		return 0, fmt.Errorf("unknown result cursor %q", trimmed)
	}
	return index, nil
}

// checkCursorRange resolves a parsed cursor against the page count of this
// call. A cursor minted for a different-sized result fails closed naming
// the cursor: the token never carries paths, scopes, or claim material, so
// naming it leaks nothing.
func checkCursorRange(cursor string, index, pages int) error {
	if index >= pages {
		return fmt.Errorf("unknown result cursor %q", strings.TrimSpace(cursor))
	}
	return nil
}
