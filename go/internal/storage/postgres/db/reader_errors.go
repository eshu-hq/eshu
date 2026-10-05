// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import "errors"

// Reader-fence sentinels. The guarded PostgreSQL reader (runtime/postgres)
// returns these when it cannot serve a fenced read, and the query layer maps a
// stale replica (ErrReaderStale) or a connection-acquisition or identity-check
// timeout inside the replay window (ErrReaderUnavailable with
// context.DeadlineExceeded) to a retryable HTTP 503 without importing the
// runtime package. They live in this dependency leaf so both sides share one error identity: the runtime
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
	// client cancellation. Only a timeout inside the replay window is transient:
	// the pool wait, the connection dial, or the identity check, each of which
	// also satisfies errors.Is(err, context.DeadlineExceeded). The query layer
	// maps ErrReaderUnavailable to a retryable 503 only together with that
	// deadline, and treats every other ErrReaderUnavailable (authentication or
	// TLS failure, connection refused, permission denied, a client disconnect)
	// as a plain failure.
	ErrReaderUnavailable = errors.New("PostgreSQL reader unavailable")
	// ErrWrongTopology reports that a PostgreSQL role, database, system
	// identity, or primary history differs from the one the access was
	// bootstrapped against: a promotion, a restore, or a different cluster. It is
	// permanent until the process restarts, so a caller must not tell a client to
	// retry shortly on its account alone. The runtime package re-exports this same
	// value so errors.Is matches in either direction (#7586).
	ErrWrongTopology = errors.New("PostgreSQL reader topology mismatch")
)

// ReaderRetryAfterSeconds is the Retry-After hint, in seconds, that every
// retryable PostgreSQL reader failure sends to HTTP clients. It is sized to the
// default reader replay window (2 s), so a retry lands after the replica has had
// one full catch-up window. It is a fixed constant: no clock is read to derive
// it. The query layer and the checkpoint middleware both use it, so the hint
// never drifts between the two.
const ReaderRetryAfterSeconds = 2
