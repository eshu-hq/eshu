// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerSupplyChainImpactAttachmentWait registers the #11105 counter on
// inst, labeled by domain and the closed outcome set deferred_in_flight,
// producer_failed, bound_exceeded.
func registerSupplyChainImpactAttachmentWait(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactAttachmentWait, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_attachment_wait_total",
		metric.WithDescription("Supply chain impact sibling-wait decisions on the same scope generation's "+
			"sbom_attestation_attachment work, by reducer domain and outcome "+
			"(deferred_in_flight/producer_failed/bound_exceeded): whether an impact pass deferred without "+
			"spending an attempt because its own attachment work was in flight, or ran without an attachment "+
			"that dead-lettered or failed, or that showed no sign of life past the bound (#11105)"),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactAttachmentWait counter: %w", err)
	}
	return nil
}
