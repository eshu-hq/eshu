// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// OpenFailureSummary classifies an Open error into redaction-safe tokens
// for the live-postgres-readiness diagnostic log (#8486).
//
// An Open failure wraps its driver cause in privateFailure, whose Error
// stays a fixed public string by design (see private_error_test.go): the
// standby refused/timeout/not-ready distinction survives only inside
// Unwrap, and formatting the chain would leak connection metadata. This
// summary instead reports closed tokens derived with errors.Is and
// errors.As, plus the failure class from readerFailureClass and the
// unwrap depth. It never formats cause text, hostnames, DSNs, or SQL.
//
// The returned line is stable for log grep: tokens are space-separated
// key=value pairs, values come from closed sets, and a nil error reports
// site=none. Call sites may log it unconditionally on the Open failure
// path; it allocates nothing observable and changes no Open semantics.
func OpenFailureSummary(err error) string {
	if err == nil {
		return "open-failure site=none class=neutral unavailable=false wrong-topology=false deadline-exceeded=false canceled=false depth=0"
	}
	var tokens strings.Builder
	fmt.Fprintf(&tokens, "open-failure site=%s class=%s", openFailureSite(err), openFailureClass(err))
	fmt.Fprintf(&tokens, " unavailable=%t", errors.Is(err, ErrReaderUnavailable))
	fmt.Fprintf(&tokens, " wrong-topology=%t", errors.Is(err, ErrWrongTopology))
	fmt.Fprintf(&tokens, " deadline-exceeded=%t", errors.Is(err, context.DeadlineExceeded))
	fmt.Fprintf(&tokens, " canceled=%t", errors.Is(err, context.Canceled))
	fmt.Fprintf(&tokens, " depth=%d", failureChainDepth(err))
	return tokens.String()
}

// openFailureSite names the outermost privateFailure stage, or unknown
// when the error carries no classified site.
func openFailureSite(err error) string {
	var failure privateError
	if !errors.As(err, &failure) {
		return "unknown"
	}
	switch failure.site {
	case failureWriterIdentity:
		return "writer-identity"
	case failureWriterPing:
		return "writer-ping"
	case failureReaderPing:
		return "reader-ping"
	case failureWriterCheckpoint:
		return "writer-checkpoint"
	case failureReaderBorrow:
		return "reader-borrow"
	case failureReaderIdentity:
		return "reader-identity"
	case failureReaderReplay:
		return "reader-replay"
	case failureReaderQuery:
		return "reader-query"
	case failureReaderRows:
		return "reader-rows"
	case failureSnapshotBegin:
		return "snapshot-begin"
	case failureSnapshotTerminal:
		return "snapshot-terminal"
	case failureRawBytes:
		return "raw-bytes"
	case failurePoolClose:
		return "pool-close"
	default:
		return "unknown"
	}
}

// openFailureClass maps the readerFailureClass verdict to its closed
// token. The classifier already understands joins, PgError codes, DNS
// errors, and context causes without formatting any of them.
func openFailureClass(err error) string {
	switch readerFailureClass(err) {
	case readerFailureTransient:
		return "transient"
	case readerFailureFatal:
		return "fatal"
	case readerFailureCanceled:
		return "canceled"
	default:
		return "neutral"
	}
}

// failureChainDepth counts nodes along the longest unwrap path, treating
// a joined level as one node plus its deepest child. Depth alone cannot
// identify the cause; it only sizes the masked chain for the log.
func failureChainDepth(err error) int {
	if err == nil {
		return 0
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		deepest := 0
		for _, cause := range joined.Unwrap() {
			if depth := failureChainDepth(cause); depth > deepest {
				deepest = depth
			}
		}
		return 1 + deepest
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return 1 + failureChainDepth(wrapped.Unwrap())
	}
	return 1
}
