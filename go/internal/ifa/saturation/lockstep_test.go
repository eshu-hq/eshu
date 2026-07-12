// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package saturation_test

import (
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestGraphWriteTimeoutStaysCountingClass locks the saturation model to the
// production retry-budget contract. The whole #3560 regression rests on
// graph_write_timeout being a COUNTING class: a write that keeps timing out
// under oversubscription must exhaust its attempt budget and dead-letter, which
// is exactly what the gated fix prevents and the ungated control reproduces. If
// graph_write_timeout were ever added to the production non-counting set
// (postgres.nonCountingReducerRetryFailureClasses), the real queue would retry
// forever instead of dead-lettering, this hermetic model's counting assumption
// would silently diverge, and the ungated flood test could false-green. Asserting
// the production predicate directly makes that drift fail here, in lockstep,
// rather than in production.
func TestGraphWriteTimeoutStaysCountingClass(t *testing.T) {
	t.Parallel()

	if postgres.IsNonCountingReducerRetryFailureClass(sourcecypher.GraphWriteTimeoutFailureClass) {
		t.Fatalf("production contract drift: %q is now a non-counting reducer retry class; "+
			"the saturation Odù models it as counting (exhausts attempts -> dead-letter). "+
			"Reconcile the model or the contract before trusting this regression.",
			sourcecypher.GraphWriteTimeoutFailureClass)
	}
}
