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
// drift, redaction-policy drops, and walker shape mismatches the moment they
// show up in real state JSON. Without this recorder, composite skips have no
// operator-visible trail and bucket E (attribute_drift) can regress.
type compositeCaptureLoggingRecorder struct {
	counter metric.Int64Counter
	logger  *slog.Logger
}

// Record implements terraformstate.CompositeCaptureRecorder.
//
// The counter carries two bounded labels: resource_type (bounded by the
// loaded schema bundle) and reason (closed enum:
// terraformstate.CompositeCaptureSkipReason* values). High-cardinality
// attribute_key, the source path, and the diagnostic error string stay in
// the structured log attrs per CLAUDE.md observability rules.
func (r compositeCaptureLoggingRecorder) Record(ctx context.Context, skip terraformstate.CompositeCaptureSkip) {
	reason := skip.Reason
	if reason == "" {
		// Defensive default: pre-reason callers (or fixtures that did not
		// set the field) get attributed to schema_unknown, which matches
		// the original counter's interpretation before the dimension was
		// added.
		reason = terraformstate.CompositeCaptureSkipReasonSchemaUnknown
	}
	if r.counter != nil {
		r.counter.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrResourceType(skip.ResourceType),
			telemetry.AttrCompositeSkipReason(reason),
		))
	}
	if r.logger != nil {
		attrs := []slog.Attr{
			slog.String(telemetry.LogKeyDriftCompositeResourceType, skip.ResourceType),
			slog.String(telemetry.LogKeyDriftCompositeAttributeKey, skip.AttributeKey),
			slog.String(telemetry.LogKeyDriftCompositePath, skip.Path),
			slog.String(telemetry.LogKeyDriftCompositeReason, reason),
		}
		if skip.Err != nil {
			attrs = append(attrs, slog.String(telemetry.LogKeyDriftCompositeError, skip.Err.Error()))
		}
		r.logger.LogAttrs(ctx, slog.LevelWarn, compositeCaptureSkipMessage(reason), attrs...)
	}
}

func compositeCaptureSkipMessage(reason string) string {
	switch reason {
	case terraformstate.CompositeCaptureSkipReasonSchemaUnknown:
		return "terraform-state composite skipped: provider schema does not cover (resource_type, attribute_key)"
	case terraformstate.CompositeCaptureSkipReasonWalkerError:
		return "terraform-state composite skipped: state JSON shape disagreed with provider schema mid-walk"
	case terraformstate.CompositeCaptureSkipReasonSensitiveSource:
		return "terraform-state composite skipped: redaction policy dropped sensitive source path"
	case terraformstate.CompositeCaptureSkipReasonUnknownRuleSet:
		return "terraform-state composite skipped: redaction rules are not initialized"
	default:
		return "terraform-state composite skipped"
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
