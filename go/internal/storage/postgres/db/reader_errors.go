// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import "errors"

// Reader-fence sentinels. The guarded PostgreSQL reader (runtime/postgres)
// returns these when it cannot serve a fenced read, and the query layer maps a
// stale replica (ErrReaderStale) or a pool-wait timeout (ErrReaderUnavailable
// with context.DeadlineExceeded) to a retryable HTTP 503 without importing the
// runtime package. They live
// in this dependency leaf so both sides share one error identity: the runtime
// package re-exports the same values, so errors.Is matches in either direction.
var (
	// ErrReaderStale reports that a reader replica did not replay to the writer
	// checkpoint within the replay timeout. The condition is transient; a retry
	// normally succeeds once replay catches up.
	ErrReaderStale = errors.New("PostgreSQL reader has not reached writer checkpoint")
	// ErrReaderUnavailable reports that a reader connection could not be
	// acquired, identified, or replay-checked. It is a classification marker,
	// not a promise of transience: runtime/postgres joins it onto every reader
	// connection, identity-query, and replay-query failure, including permanent
	// ones (authentication, TLS, connection refused, permission denied) and a
	// client cancellation. Only the pool-wait timeout, which also satisfies
	// errors.Is(err, context.DeadlineExceeded), is transient; the query layer
	// maps ErrReaderUnavailable to a retryable 503 only together with that
	// deadline, and treats every other ErrReaderUnavailable as a plain failure.
	ErrReaderUnavailable = errors.New("PostgreSQL reader unavailable")
)

// ReaderRetryAfterSeconds is the Retry-After hint, in seconds, that every
// retryable PostgreSQL reader failure sends to HTTP clients. It is sized to the
// default reader replay window (2 s), so a retry lands after the replica has had
// one full catch-up window. It is a fixed constant: no clock is read to derive
// it. The query layer and the checkpoint middleware both use it, so the hint
// never drifts between the two.
const ReaderRetryAfterSeconds = 2
