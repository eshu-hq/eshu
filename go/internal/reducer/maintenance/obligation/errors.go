// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package obligation

import "errors"

// ErrLeaseLost marks a Finalize whose lease expired inside its own
// transaction after the wake ran; the store rolled it back.
var ErrLeaseLost = errors.New("activation obligation lease lost")

// ErrFinalizeLockTimeout marks a Finalize (or an inapplicable
// retire) that waited longer than its lock_timeout for the scope or
// obligation row, typically behind an ingestion commit or a projector Ack on
// the same scope. The transaction rolled back and wrote nothing; the
// obligation stays leased and the next claimer settles it after the lease.
// The runner counts it under its own reason, finalize_lock_timeout, at Warn,
// so expected contention is not read as a broken statement (#7584). The
// store adapter returns it wrapped around the database error.
var ErrFinalizeLockTimeout = errors.New("activation obligation finalize lock timeout")

// ErrInapplicable is returned (wrapped) by a Maintainer
// when the owed partition is active but maps to no repository in the shipped
// active-repository read, so no pass can ever publish its phase (a repo_id
// collision loser). The runner retires the obligation as inapplicable through
// the token-fenced store method; it is not a maintenance failure.
var ErrInapplicable = errors.New("activation obligation is inapplicable: no repository maps to the owed partition")

// HoldError is a maintainer refusal that holds the obligation: the
// runner keeps the lease, retries at lease cadence, never runs a fallback
// pass, and counts it under its closed reason. Every hold reason clears on
// the epoch whole pass (#7584). Build one with
// Hold; match one with errors.As or with errors.Is against
// ErrCatalogChanged.
type HoldError struct {
	reason string
	cause  error
}

// Error implements error.
func (e *HoldError) Error() string {
	if e.cause == nil {
		return "activation maintenance held: " + e.reason
	}
	return "activation maintenance held: " + e.reason + ": " + e.cause.Error()
}

// Unwrap returns the maintainer's own refusal.
func (e *HoldError) Unwrap() error { return e.cause }

// Reason returns the closed hold reason, safe as a metric label.
func (e *HoldError) Reason() string { return e.reason }

// Is matches another hold with the same reason, so errors.Is(err,
// ErrCatalogChanged) holds for every catalog_changed hold.
func (e *HoldError) Is(target error) bool {
	other, ok := target.(*HoldError)
	return ok && other.reason == e.reason
}

// Activation hold reasons. The set is closed: Hold refuses any
// other reason.
const (
	// HoldCatalogChanged: the repository catalog changed since the
	// published memos were written; the commit that changed it triggers the
	// epoch whole pass.
	HoldCatalogChanged = "catalog_changed"
	// HoldNoMemoBaseline: no active partition holds a memo row yet,
	// so a catalog change cannot be detected until a whole pass writes one.
	HoldNoMemoBaseline = "no_memo_baseline"
	// HoldClosureTooDeep: promoting dependent partitions did not
	// settle within the pass's round bound.
	HoldClosureTooDeep = "closure_too_deep"
)

// ErrCatalogChanged matches (errors.Is) every catalog_changed hold.
var ErrCatalogChanged error = &HoldError{reason: HoldCatalogChanged}

// HoldReasons returns the sorted closed set of hold reasons.
func HoldReasons() []string {
	return []string{HoldCatalogChanged, HoldClosureTooDeep, HoldNoMemoBaseline}
}

// Hold wraps a maintainer refusal as a hold with reason. An
// unknown reason is not a hold: the cause is returned unchanged and the
// runner counts it as a maintenance failure, so the label set stays closed.
func Hold(reason string, cause error) error {
	for _, known := range HoldReasons() {
		if reason == known {
			return &HoldError{reason: reason, cause: cause}
		}
	}
	return cause
}
