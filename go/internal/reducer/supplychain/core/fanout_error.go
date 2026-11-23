// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "errors"

// ErrFanOutReadTimeout marks a holder read that exceeded its bounded statement
// timeout. The pass fails with it so the queue retries; the counter reports it
// as read_timeout rather than read_error.
var ErrFanOutReadTimeout = errors.New("supply chain impact holder read timed out")

// ErrSupportsNotReady marks a holder, seed or fence read that refused to run
// because the supports table it reads is not ready: the backfill marker for the
// binary's contract version is not complete, a newer contract's marker exists, or
// the old-writer fence still holds a dirty scope (#11197). The reads no longer
// fall back to the finding facts, so a partial table must never answer; the pass
// fails with it, the queue retries, and the counter reports it as
// supports_not_ready rather than read_error.
var ErrSupportsNotReady = errors.New("supply chain impact supports are not ready")

// SupplyChainImpactFanOutFailureClass classifies a pass that failed in its holder
// fan-out. It is a counting class on purpose: the queue retries the pass up to
// its attempt budget and then dead-letters it, which is loud and replayable,
// whereas a non-counting class would retry a persistent failure forever.
const SupplyChainImpactFanOutFailureClass = "supply_chain_impact_fanout_failed"

// RetryableFanOutError marks err retryable and self-classified like the
// handler's own fan-out failures. The store returns it for a lost lease inside the
// writer's transaction, where a plain error would reach the queue as terminal.
func RetryableFanOutError(err error) error { return fanOutError{err} }

// fanOutError marks a fan-out failure as retryable and self-classified. The
// queue dead-letters any error that does not report Retryable() == true, so
// without this a single transient holder-read timeout, after the writer already
// committed, would dead-letter the pass instead of retrying it.
type fanOutError struct{ err error }

func (e fanOutError) Error() string { return e.err.Error() }
func (e fanOutError) Unwrap() error { return e.err }

// Retryable reports that the queue should retry the whole pass.
func (fanOutError) Retryable() bool { return true }

// FailureClass names the durable failure class for the retrying row.
func (fanOutError) FailureClass() string { return SupplyChainImpactFanOutFailureClass }
