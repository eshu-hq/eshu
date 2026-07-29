// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// writeUnresolvedOwner persists one durable "unresolved" finding for the
// whole state-snapshot scope when backend-owner resolution finds zero
// candidate config repos (tfstatebackend.ErrNoConfigRepoOwnsBackend).
// Mirrors writeAmbiguousOwner in every respect except which write mode it
// requests: no-op when Writer is nil (counters/logs-only mode); write
// failures are logged and counted, never returned as a Handle() error,
// because "no repo owns this backend" is already a non-fatal,
// operator-actionable rejection (DriftRejection's contract,
// Result{Status: Succeeded}) with no retry that could fix it — failing the
// whole intent over a best-effort durability write would turn that warning
// into a retry storm.
//
// Issue #5594: before this, ErrNoConfigRepoOwnsBackend was log-only, so a
// caller reading POST /api/v0/terraform/config-state-drift/findings for this
// scope could not distinguish "evaluated, no drift" (zero findings because
// resolution succeeded and nothing disagreed) from "ownership never
// resolved at all" (also zero findings, because no per-address evidence was
// ever loaded) — both looked like an empty, clean result. This durable row
// (outcome "unresolved") closes that gap the same way writeAmbiguousOwner
// already closed it for the ambiguous case.
func (h TerraformConfigStateDriftHandler) writeUnresolvedOwner(
	ctx context.Context,
	intent Intent,
	backendKind string,
	locatorHash string,
) {
	if h.Writer == nil {
		return
	}
	_, writeErr := h.Writer.WriteTerraformConfigStateDriftFindings(ctx, TerraformConfigStateDriftWrite{
		IntentID:        intent.IntentID,
		ScopeID:         intent.ScopeID,
		GenerationID:    intent.GenerationID,
		SourceSystem:    intent.SourceSystem,
		Cause:           intent.Cause,
		BackendKind:     backendKind,
		LocatorHash:     locatorHash,
		UnresolvedOwner: true,
	})
	if writeErr == nil {
		return
	}
	if h.Instruments != nil && h.Instruments.DriftUnresolvedOwnerWriteFailed != nil {
		h.Instruments.DriftUnresolvedOwnerWriteFailed.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionPack, rules.TerraformConfigStateDriftPackName),
		))
	}
	if h.Logger != nil {
		h.Logger.LogAttrs(
			ctx, slog.LevelWarn, "drift unresolved owner durable write failed",
			log.Domain(string(intent.Domain)),
			log.ScopeID(intent.ScopeID),
			log.GenerationID(intent.GenerationID),
			slog.String("write.error", writeErr.Error()),
		)
	}
}
