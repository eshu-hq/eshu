// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfconfigstate

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// writeUnresolvedOwner persists one durable "unresolved" finding for the
// whole state-snapshot scope when backend-owner resolution finds zero
// candidate config repos (tfstatebackend.ErrNoConfigRepoOwnsBackend). No-op
// when Writer is nil (counters/logs-only mode).
//
// A write failure is returned to the caller as an error, which Handle()
// propagates as a retriable Handle() failure — deliberately NOT swallowed
// the way writeAmbiguousOwner swallows its own write failure. The two cases
// differ in what recovers a lost write: a resolvable backend whose owner
// later changes gets a fresh chance on the next apply's new state_snapshot
// generation regardless of whether this write succeeded, but a
// permanently-unresolved backend's state produces no such future generation
// to retry against — losing this write is not "eventually corrected," it is
// simply lost, with the durable "unresolved" row (and everything reading it)
// silently reverting to indistinguishable-from-empty. This matches the
// existing "exact"-outcome write path in Handle(), which already treats an
// identical WriteTerraformConfigStateDriftFindings failure as fatal for the
// same reason.
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
	intent reducercontract.Intent,
	backendKind string,
	locatorHash string,
) error {
	if h.Writer == nil {
		return nil
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
		return nil
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
	return writeErr
}
