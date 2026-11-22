// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reducerAdmissionBackoffCap is the ceiling of the read interval while the
// gate defers (#11233). RED scaffolding: not used yet.
const reducerAdmissionBackoffCap = 10 * time.Second

// admissionDepthHolder will hold the process-wide queue-depth read (#11233).
// RED scaffolding: it holds nothing and the gate still reads per decision.
type admissionDepthHolder struct {
	queueDepth *admissionSharedRead[int64]
}

// admissionSharedRead will be the single-flight bounded-age read of one gate
// signal. RED scaffolding: no sharing.
type admissionSharedRead[T any] struct{}

// waiting reports how many callers wait on the in-flight read.
func (s *admissionSharedRead[T]) waiting() int { return 0 }

func newAdmissionDepthHolder(
	_ reducerAdmissionDepthReader,
	_ reducerAdmissionConfig,
	_ func() time.Time,
	_ *telemetry.Instruments,
) *admissionDepthHolder {
	return &admissionDepthHolder{queueDepth: &admissionSharedRead[int64]{}}
}
