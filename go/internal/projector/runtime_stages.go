package projector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (r Runtime) writeContentProjection(ctx context.Context, scopeValue scope.IngestionScope, mat content.Materialization) (content.Result, error) {
	if len(mat.Records) == 0 && len(mat.Entities) == 0 {
		return content.Result{}, nil
	}
	if r.ContentWriter == nil {
		return content.Result{}, errors.New("content writer is required when content rows are present")
	}

	contentStart := time.Now()
	contentResult, err := r.ContentWriter.Write(ctx, mat)
	if err != nil {
		return content.Result{}, fmt.Errorf("write content materialization: %w", err)
	}
	if r.Instruments != nil {
		r.Instruments.ProjectorStageDuration.Record(ctx, time.Since(contentStart).Seconds(), metric.WithAttributes(
			telemetry.AttrScopeID(scopeValue.ScopeID),
			attribute.String("stage", "content_write"),
		))
	}
	r.logRuntimeStage(ctx, scopeValue, mat.GenerationID, "content_write", contentStart,
		"content_record_count", len(mat.Records),
		"content_entity_count", len(mat.Entities),
		"deleted_count", contentResult.DeletedCount,
	)

	return contentResult, nil
}

func (r Runtime) writeCanonicalProjection(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generationID string,
	inputFacts []facts.Envelope,
	mat CanonicalMaterialization,
) error {
	if mat.IsEmpty() {
		return nil
	}
	if r.CanonicalWriter == nil {
		return errors.New("canonical writer is required when canonical data is present")
	}

	canonicalStart := time.Now()
	if r.Tracer != nil {
		var canonicalSpan trace.Span
		ctx, canonicalSpan = r.Tracer.Start(ctx, telemetry.SpanCanonicalProjection)
		defer canonicalSpan.End()
	}

	if err := r.CanonicalWriter.Write(ctx, mat); err != nil {
		return fmt.Errorf("write canonical projection: %w", err)
	}
	if err := r.publishCanonicalGraphPhases(ctx, generationID, inputFacts); err != nil {
		return fmt.Errorf("publish canonical graph phases: %w", err)
	}

	if r.Instruments != nil {
		canonicalDur := time.Since(canonicalStart).Seconds()
		r.Instruments.CanonicalProjectionDuration.Record(ctx, canonicalDur, metric.WithAttributes(
			telemetry.AttrScopeID(scopeValue.ScopeID),
		))
		r.Instruments.CanonicalWrites.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrScopeID(scopeValue.ScopeID),
		))
		r.Instruments.ProjectorStageDuration.Record(ctx, canonicalDur, metric.WithAttributes(
			telemetry.AttrScopeID(scopeValue.ScopeID),
			attribute.String("stage", "canonical_write"),
		))
	}
	r.logRuntimeStage(ctx, scopeValue, generationID, "canonical_write", canonicalStart,
		"repository_count", canonicalRepositoryCount(mat),
		"directory_count", len(mat.Directories),
		"file_count", len(mat.Files),
		"entity_count", len(mat.Entities),
		"module_count", len(mat.Modules),
		"import_count", len(mat.Imports),
		"terraform_state_resource_count", len(mat.TerraformStateResources),
		"terraform_state_module_count", len(mat.TerraformStateModules),
		"terraform_state_output_count", len(mat.TerraformStateOutputs),
	)

	return nil
}

func canonicalRepositoryCount(mat CanonicalMaterialization) int {
	if mat.Repository == nil {
		return 0
	}
	return 1
}
