package tfstateruntime

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// compositeCaptureLoggingRecorder is the production
// terraformstate.CompositeCaptureRecorder used by ClaimedSource. It forwards
// every Record call to the eshu_dp_drift_schema_unknown_composite_total
// counter and to a structured slog.Warn so operators can see provider-schema
// drift the moment it shows up in real state JSON. Without this recorder, the
// streaming nested walker silently skips composites the bundle does not know
// about and bucket E (attribute_drift) regresses for those attributes.
type compositeCaptureLoggingRecorder struct {
	counter metric.Int64Counter
	logger  *slog.Logger
}

// Record implements terraformstate.CompositeCaptureRecorder.
//
// The counter carries the resource_type label only; high-cardinality
// attribute_key, the source path, and the diagnostic error string stay in the
// structured log attrs per CLAUDE.md observability rules.
func (r compositeCaptureLoggingRecorder) Record(ctx context.Context, skip terraformstate.CompositeCaptureSkip) {
	if r.counter != nil {
		r.counter.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrResourceType(skip.ResourceType),
		))
	}
	if r.logger != nil {
		attrs := []slog.Attr{
			slog.String(telemetry.LogKeyDriftCompositeResourceType, skip.ResourceType),
			slog.String(telemetry.LogKeyDriftCompositeAttributeKey, skip.AttributeKey),
			slog.String(telemetry.LogKeyDriftCompositePath, skip.Path),
		}
		if skip.Err != nil {
			attrs = append(attrs, slog.String(telemetry.LogKeyDriftCompositeError, skip.Err.Error()))
		}
		r.logger.LogAttrs(ctx, slog.LevelWarn,
			"terraform-state composite skipped: provider schema does not cover (resource_type, attribute_key)",
			attrs...,
		)
	}
}

// newCompositeCaptureRecorder wires the ClaimedSource's Instruments and Logger
// into the streaming nested walker's CompositeCaptureRecorder seam. A nil
// Instruments or nil counter returns nil so the parser keeps treating the
// recorder as a no-op during fixtures and early bootstrap.
func (s ClaimedSource) newCompositeCaptureRecorder() terraformstate.CompositeCaptureRecorder {
	if s.Instruments == nil || s.Instruments.DriftSchemaUnknownComposite == nil {
		return nil
	}
	return compositeCaptureLoggingRecorder{
		counter: s.Instruments.DriftSchemaUnknownComposite,
		logger:  s.Logger,
	}
}
