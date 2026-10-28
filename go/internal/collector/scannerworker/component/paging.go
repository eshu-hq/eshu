// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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

// formatCursor mints the opaque resume token for one page: the next page
// index plus the claim observation time minted on the first page. The host
// echoes it back verbatim on the continuation call; only this component
// interprets it. Carrying the observation time in the token keeps every
// page's Generation.ObservedAt identical across the stateless
// re-invocations one claim reassembles, which the host generation pin
// (extensionhost sameGeneration) requires. The timestamp is the runner's
// own observation time, never claim material, so it stays safe to name in
// errors under the package leak rules.
func formatCursor(pageIndex int, observedAt time.Time) string {
	return "page-" + strconv.Itoa(pageIndex) + "." + strconv.FormatInt(observedAt.UTC().UnixNano(), 10)
}

// parseCursor resolves a continuation cursor to the page index it names and
// the claim observation time it carries, without range context. Format
// forgeries fail here, before any analysis runs — including cursors minted
// before the observation time traveled in the token, since falling back to
// the call clock for those would reintroduce cross-page drift.
func parseCursor(cursor string) (int, time.Time, error) {
	trimmed := strings.TrimSpace(cursor)
	rest, ok := strings.CutPrefix(trimmed, "page-")
	if !ok {
		return 0, time.Time{}, fmt.Errorf("unknown result cursor %q", trimmed)
	}
	head, tail, ok := strings.Cut(rest, ".")
	if !ok {
		return 0, time.Time{}, fmt.Errorf("unknown result cursor %q", trimmed)
	}
	index, err := strconv.Atoi(head)
	if err != nil || index < 0 {
		return 0, time.Time{}, fmt.Errorf("unknown result cursor %q", trimmed)
	}
	nanos, err := strconv.ParseInt(tail, 10, 64)
	if err != nil || nanos <= 0 {
		return 0, time.Time{}, fmt.Errorf("unknown result cursor %q", trimmed)
	}
	return index, time.Unix(0, nanos).UTC(), nil
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
