// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerCrossScopeCompletionSuppressedInstruments registers the counter of
// producer passes whose acknowledgement skips the cross-scope completion event
// because the pass wrote nothing a consumer reads (#10972 PR-4), labeled by
// producer domain only. ci_cd_run_correlation is the one domain that counts it
// today; a pass with a pending completion-owed marker still emits, so the
// counter can overstate suppression by the rare commit-then-crash retries.
func registerCrossScopeCompletionSuppressedInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CrossScopeCompletionSuppressed, err = meter.Int64Counter(
		"eshu_dp_cross_scope_completion_suppressed_total",
		metric.WithDescription("Producer passes whose acknowledgement adds no cross-scope completion event because the pass changed nothing a consumer reads, by producer domain (#10972)"),
	); err != nil {
		return fmt.Errorf("register CrossScopeCompletionSuppressed counter: %w", err)
	}
	return nil
}
