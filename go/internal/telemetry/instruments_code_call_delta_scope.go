// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerCodeCallDeltaScopeUnusable registers the #10023 counter of
// delta-generation repositories whose code-call file scope could not be
// built. The code_call_materialization handler records it once per such
// repository before it fails the work item closed (non-retryable, failure
// class code_call_delta_scope_unusable), so the item dead-letters on its
// first attempt instead of rewriting the repository's CALLS edges from the
// delta's rows only. The reason label is the closed codecall
// DeltaScopeUnusableReason set (missing_local_path, unnormalizable_path,
// no_changed_paths). Repository, scope, and generation ids never become
// labels; they travel on the ERROR log and the dead-letter row.
//
// It stays at zero until #10049: the git collector emits no
// code_call_materialization marker on a delta generation yet.
func registerCodeCallDeltaScopeUnusable(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CodeCallDeltaScopeUnusable, err = meter.Int64Counter(
		"eshu_dp_code_call_delta_scope_unusable_total",
		metric.WithDescription("Delta-generation repositories whose code-call file scope could not be built (#10023), "+
			"by bounded reason (missing_local_path, unnormalizable_path, no_changed_paths); "+
			"each one fails its code_call_materialization work item closed to a dead letter "+
			"(failure_class code_call_delta_scope_unusable) and its CALLS edges wait for the next full generation"),
	); err != nil {
		return fmt.Errorf("register CodeCallDeltaScopeUnusable counter: %w", err)
	}
	return nil
}
