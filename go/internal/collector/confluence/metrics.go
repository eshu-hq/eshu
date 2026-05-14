package confluence

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

func (s *Source) recordPermissionDeniedPage(ctx context.Context, operation string) {
	if s.Instruments == nil || s.Instruments.ConfluencePermissionDeniedPages == nil {
		return
	}
	s.Instruments.ConfluencePermissionDeniedPages.Add(
		ctx,
		1,
		metric.WithAttributes(telemetry.AttrOperation(operation)),
	)
}

func (s *Source) recordFactMetrics(ctx context.Context, envelopes []facts.Envelope) {
	if s.Instruments == nil {
		return
	}
	var documentCount, sectionCount, linkCount int64
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.DocumentationDocumentFactKind:
			documentCount++
		case facts.DocumentationSectionFactKind:
			sectionCount++
		case facts.DocumentationLinkFactKind:
			linkCount++
		}
	}
	attrs := metric.WithAttributes(telemetry.AttrResult("success"))
	if documentCount > 0 && s.Instruments.ConfluenceDocumentsObserved != nil {
		s.Instruments.ConfluenceDocumentsObserved.Add(ctx, documentCount, attrs)
	}
	if sectionCount > 0 && s.Instruments.ConfluenceSectionsEmitted != nil {
		s.Instruments.ConfluenceSectionsEmitted.Add(ctx, sectionCount, attrs)
	}
	if linkCount > 0 && s.Instruments.ConfluenceLinksEmitted != nil {
		s.Instruments.ConfluenceLinksEmitted.Add(ctx, linkCount, attrs)
	}
}

func (s *Source) recordSyncFailure(ctx context.Context, failureClass string) {
	if s.Instruments == nil || s.Instruments.ConfluenceSyncFailures == nil {
		return
	}
	s.Instruments.ConfluenceSyncFailures.Add(
		ctx,
		1,
		metric.WithAttributes(telemetry.AttrFailureClass(failureClass)),
	)
}
