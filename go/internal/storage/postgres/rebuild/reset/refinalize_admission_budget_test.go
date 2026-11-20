// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"strings"
	"testing"
)

// TestRefinalizeResetsAdmissionCounterWithAttemptCount pins the #10671
// paired-budget invariant on the refinalize re-enqueue: a rebuild restart
// resets both counters, so a replayed row starts both budgets fresh. A
// reset attempt_count with a kept admission_wait_attempts would refund a
// negative projection budget (AttemptCount-AdmissionWaitAttempts < 0,
// retrying everything) or carry a spent admission budget into the fresh
// cycle (dead-letter on the first new wait).
func TestRefinalizeResetsAdmissionCounterWithAttemptCount(t *testing.T) {
	t.Parallel()

	if !strings.Contains(refinalizeScopeProjectionsQuery, "admission_wait_attempts = 0") {
		t.Fatalf("refinalize query must reset admission_wait_attempts:\n%s", refinalizeScopeProjectionsQuery)
	}
	insert, _, _ := strings.Cut(refinalizeScopeProjectionsQuery, "ON CONFLICT")
	for _, want := range []string{"attempt_count,", "admission_wait_attempts,"} {
		if !strings.Contains(insert, want) {
			t.Fatalf("refinalize INSERT column list missing %q", want)
		}
	}
}
