// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The accessors the fan-out reads off the claiming item's payload: its origin
// stamp, its owed marker, and the one-hop decision they imply. Split out of
// fanout.go to keep that file under the repo's 500-line cap.

// fanOutOriginStamped reports whether the intent's item carries a fan-out
// origin stamp.
func fanOutOriginStamped(intent reducercontract.Intent) bool {
	value, ok := intent.Payload[PayloadKeyFanOutOrigin]
	return ok && value != nil
}

// fanOutOwed reports whether the intent's item carries the owed marker: a
// commit of this item changed rows and its fan-out has not finished.
func fanOutOwed(intent reducercontract.Intent) bool {
	owed, _ := intent.Payload[PayloadKeyFanOutOwed].(bool)
	return owed
}

// fanOutOneHop reports whether this pass is the single re-derivation a fan-out
// asked for, which re-derives its copy and stops. That is the attempt the replay
// created: a reopen of a succeeded item and the replay of a flagged one both
// leave attempt_count at 0, so the claim is attempt 1. A stamp that reaches a
// later attempt was written while a genuine pass was running and that pass then
// failed or crashed, so the retry is a genuine pass again. An owed marker means
// this item committed changes it has not finished fanning out, so it is genuine
// too, whatever stamp it carries.
func fanOutOneHop(intent reducercontract.Intent) bool {
	return fanOutOriginStamped(intent) && intent.AttemptCount <= 1 && !fanOutOwed(intent)
}

// fanOutOriginOf returns the stamp map the intent carries, or nil.
func fanOutOriginOf(intent reducercontract.Intent) map[string]any {
	stamp, _ := intent.Payload[PayloadKeyFanOutOrigin].(map[string]any)
	return stamp
}

// owedRecorder returns the recorder the writer runs inside its transaction,
// or nil when there is no port. A one-hop re-derivation records too: it fans
// out nothing of its own rewrite, but since #10411 it owes the advisory-group
// enqueue that runs after the commit, and the marker is the only durable
// record that the commit happened if the enqueue (or the process) fails
// first. The fan-out's clear conditions mirror the writer's set condition,
// so a completed one-hop pass leaves no marker behind.
func (h SupplyChainImpactHandler) owedRecorder(intent reducercontract.Intent) FanOutOwedRecorder {
	if h.FanOut == nil {
		return nil
	}
	return h.FanOut.OwedRecorder(intent)
}
