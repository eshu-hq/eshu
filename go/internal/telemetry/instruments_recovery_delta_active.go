// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerRecoveryDeltaActiveScopes registers the #7797 counter of scopes a
// refinalize re-enqueued through a delta generation, by outcome.
func registerRecoveryDeltaActiveScopes(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.RecoveryDeltaActiveScopes, err = meter.Int64Counter(
		"eshu_dp_recovery_delta_active_scopes_total",
		metric.WithDescription("Total scopes an operator refinalize re-enqueued through a delta generation, "+
			"which stay incomplete in the graph until a full generation activates, by outcome "+
			"(reindex_requested/reindex_unsupported)"),
	); err != nil {
		return fmt.Errorf("register RecoveryDeltaActiveScopes counter: %w", err)
	}
	return nil
}
