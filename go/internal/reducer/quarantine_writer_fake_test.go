// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// fakeQuarantinedFactWriter is the in-memory factdecode.QuarantinedFactWriter the reducer
// batch tests assert against. It stayed in this package when the quarantine
// writer itself moved to internal/reducer/factdecode (#6061), because its only
// remaining callers are reducer-root tests.
type fakeQuarantinedFactWriter struct {
	writes    [][]factdecode.QuarantinedFactRecord
	failNext  bool
	callCount int
}

func (w *fakeQuarantinedFactWriter) WriteQuarantinedFacts(_ context.Context, records []factdecode.QuarantinedFactRecord) error {
	w.callCount++
	if w.failNext {
		w.failNext = false
		return errors.New("simulated durable write failure")
	}
	// Copy defensively so a caller mutating its slice afterward cannot
	// retroactively change what this fake observed.
	cp := make([]factdecode.QuarantinedFactRecord, len(records))
	copy(cp, records)
	w.writes = append(w.writes, cp)
	return nil
}
