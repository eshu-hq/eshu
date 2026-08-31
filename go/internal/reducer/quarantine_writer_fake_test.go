// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
)

// fakeQuarantinedFactWriter is the in-memory QuarantinedFactWriter the reducer
// batch tests assert against. It stayed in this package when the quarantine
// writer itself moved to internal/reducer/factdecode (#6061), because its only
// remaining callers are reducer-root tests.
type fakeQuarantinedFactWriter struct {
	writes    [][]QuarantinedFactRecord
	failNext  bool
	callCount int
}

func (w *fakeQuarantinedFactWriter) WriteQuarantinedFacts(_ context.Context, records []QuarantinedFactRecord) error {
	w.callCount++
	if w.failNext {
		w.failNext = false
		return errors.New("simulated durable write failure")
	}
	// Copy defensively so a caller mutating its slice afterward cannot
	// retroactively change what this fake observed.
	cp := make([]QuarantinedFactRecord, len(records))
	copy(cp, records)
	w.writes = append(w.writes, cp)
	return nil
}
