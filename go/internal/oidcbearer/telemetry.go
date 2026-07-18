// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Outcome values for eshu_dp_oidc_bearer_validation_total{outcome}. This is
// the complete, closed set: every code path that returns from
// ResolveScopedToken having actually consulted a matched provider's verifier
// records exactly one of these. Falling through (non-JWT-shaped credential,
// or a JWT-shaped credential whose issuer matches no enabled provider before
// AC's denial semantics for an unmatched issuer are reached) is NOT resolved
// through this set — see resolver.go's doc comment on ResolveScopedToken for
// which paths never touch the counter at all.
const (
	outcomeValid            = "valid"
	outcomeExpired          = "expired"
	outcomeWrongAudience    = "wrong_audience"
	outcomeUnknownIssuer    = "unknown_issuer"
	outcomeBadSignature     = "bad_signature"
	outcomeMalformed        = "malformed"
	outcomeJWKSFetchFailure = "jwks_fetch_failure"
	outcomeNoGrants         = "no_grants"
)

// resolveSpanName is the OTEL span name for one ResolveScopedToken call that
// reached provider matching (i.e. did not fall through at the JWT-shape
// gate). It is documented in docs/public/observability/telemetry-coverage.md
// under the OTEL Span Names section.
const resolveSpanName = "oidcbearer.resolve"

// recordOutcome increments eshu_dp_oidc_bearer_validation_total{outcome} when
// instruments are wired. Instruments are optional (nil-safe) so tests and
// any future caller that does not need metrics can construct a Resolver
// without a full telemetry.Instruments.
func (c *cache) recordOutcome(ctx context.Context, outcome string) {
	if c.instruments == nil || c.instruments.OIDCBearerValidationTotal == nil {
		return
	}
	c.instruments.OIDCBearerValidationTotal.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(outcome)))
}

// startResolveSpan starts the oidcbearer.resolve span. Callers must End it.
func startResolveSpan(ctx context.Context) (context.Context, trace.Span) {
	return otel.Tracer(telemetry.DefaultSignalName).Start(ctx, resolveSpanName)
}
