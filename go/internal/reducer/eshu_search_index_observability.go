package reducer

import (
	"context"
	"database/sql"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func (w PostgresEshuSearchDocumentWriter) startSearchIndexWriteSpan(
	ctx context.Context,
	scopeID string,
	generationID string,
	documentCount int,
) (context.Context, trace.Span) {
	if w.Tracer == nil {
		return ctx, nil
	}
	return w.Tracer.Start(
		ctx, telemetry.SpanReducerEshuSearchIndexWrite,
		trace.WithAttributes(
			attribute.String(telemetry.MetricDimensionDomain, string(DomainEshuSearchDocument)),
			attribute.String(telemetry.MetricDimensionScopeID, scopeID),
			attribute.String(telemetry.MetricDimensionGenerationID, generationID),
			attribute.Int("document_count", documentCount),
		),
	)
}

func (w PostgresEshuSearchDocumentWriter) recordSearchIndexMutation(
	ctx context.Context,
	kind string,
	operation string,
	count int64,
) {
	if count <= 0 || w.Instruments == nil || w.Instruments.SearchIndexMutations == nil {
		return
	}
	w.Instruments.SearchIndexMutations.Add(ctx, count, metric.WithAttributes(
		telemetry.AttrDomain(string(DomainEshuSearchDocument)),
		telemetry.AttrKind(kind),
		telemetry.AttrOperation(operation),
		telemetry.AttrResult("success"),
	))
}

func (w PostgresEshuSearchDocumentWriter) recordSearchIndexError(ctx context.Context, operation string) {
	if operation == "" || w.Instruments == nil || w.Instruments.SearchIndexErrors == nil {
		return
	}
	w.Instruments.SearchIndexErrors.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(DomainEshuSearchDocument)),
		telemetry.AttrOperation(operation),
	))
}

func (w PostgresEshuSearchDocumentWriter) recordSearchIndexWriteDuration(
	ctx context.Context,
	duration time.Duration,
	result string,
) {
	if w.Instruments == nil || w.Instruments.SearchIndexWriteDuration == nil {
		return
	}
	w.Instruments.SearchIndexWriteDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		telemetry.AttrDomain(string(DomainEshuSearchDocument)),
		telemetry.AttrResult(result),
	))
}

func rowsAffected(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	affected, err := result.RowsAffected()
	if err != nil || affected <= 0 {
		return 0
	}
	return affected
}
