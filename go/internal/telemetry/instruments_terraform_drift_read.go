// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerTerraformDriftReadInstruments registers the delta-base union
// counter for the Terraform drift overlay readers (issue #10006) on inst.
// Each reader (canonical backend rows, backend facts, config resources)
// records one decision per delta-active scope it reached, mirroring the
// manifest reader's PackageManifestDependencyReadDeltaUnion contract.
func registerTerraformDriftReadInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.TerraformDriftReadDeltaUnion, err = meter.Int64Counter(
		"eshu_dp_terraform_drift_read_delta_union_total",
		metric.WithDescription("Delta-base union decisions of Terraform drift overlay reads, one per delta-active scope a read reached (union_resolve_failed: once per failed read), by read and outcome (#10006)"),
	); err != nil {
		return fmt.Errorf("register TerraformDriftReadDeltaUnion counter: %w", err)
	}
	return nil
}
