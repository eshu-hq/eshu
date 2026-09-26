// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure

import "errors"

// ErrWorkSuperseded reports that a claimed projector generation was replaced
// by a newer same-scope generation and should stop without acking or failing.
// Heartbeat returns it for a live older generation. Ack returns it when the
// generation is already superseded: Ack refused to re-activate it and marked
// the work item superseded (#7130).
var ErrWorkSuperseded = errors.New("projector work superseded")

// ErrWorkClaimLost reports that another attempt now owns a claimed projector
// work item, or that it reached a terminal state, so this attempt must drop it
// without acking or failing. The current owner completes the generation.
var ErrWorkClaimLost = errors.New("projector work claim lost")

// ErrWorkAckDeferred reports that Ack could not take the scope row within its
// lock timeout, usually because an ingestion commit for the same scope holds it
// while streaming facts. Ack changed nothing and the attempt still owns the
// work item, so the caller renews the lease and retries the Ack.
var ErrWorkAckDeferred = errors.New("projector work ack deferred: scope busy")

// ErrWorkClaimConflict reports that a claim statement lost a transient
// database lock conflict (deadlock or serialization failure) on every bounded
// retry. The statement rolled back, so no work item changed; the worker waits
// one poll interval and claims again instead of stopping its siblings.
var ErrWorkClaimConflict = errors.New("projector work claim conflict")
