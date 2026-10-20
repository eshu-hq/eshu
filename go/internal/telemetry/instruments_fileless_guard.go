// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerCanonicalFilelessWipeGuard registers the file-less wipe guard
// counter (#8657) on inst.
func registerCanonicalFilelessWipeGuard(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CanonicalFilelessWipeGuard, err = meter.Int64Counter(
		"eshu_dp_canonical_fileless_wipe_guard_total",
		metric.WithDescription("Canonical writer file-less wipe guard decisions, by closed outcome (refused_prior_files, probe_unsupported, probe_failed) (#8657)"),
	); err != nil {
		return fmt.Errorf("register CanonicalFilelessWipeGuard counter: %w", err)
	}
	return nil
}
