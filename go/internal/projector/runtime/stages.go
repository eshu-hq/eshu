// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/decode"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (r Runtime) writeContentProjection(ctx context.Context, scopeValue scope.IngestionScope, mat content.Materialization) (content.Result, error) {
	if len(mat.Records) == 0 && len(mat.Entities) == 0 && len(mat.RepositoryRefs) == 0 {
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
	if contentResult.FingerprintsChanged {
		if err := r.enqueueDriftedIntent(ctx, scopeValue, mat); err != nil {
			return content.Result{}, err
		}
	}
	if r.Instruments != nil {
		r.Instruments.ProjectorStageDuration.Record(ctx, time.Since(contentStart).Seconds(), metric.WithAttributes(
			telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
			attribute.String("stage", "content_write"),
		))
	}
	r.logRuntimeStage(
		ctx, scopeValue, mat.GenerationID, "content_write", contentStart,
		"content_record_count", len(mat.Records),
		"content_entity_count", len(mat.Entities),
		"content_repository_ref_count", len(mat.RepositoryRefs),
		"deleted_count", contentResult.DeletedCount,
	)

	return contentResult, nil
}

// enqueueDriftedIntent admits one code_drifted reducer intent for a content
// generation that (re)published fingerprint side-table truth (epic #6833,
// child #6837). The queue dedupes by (domain, scope_id, generation_id), so
// an identical re-publish of the same generation collapses onto the existing
// work item instead of duplicating handler work. A nil IntentWriter is a
// wiring gap: fail closed rather than silently drop the generation, mirroring
// the required-writer check on the projection intent path in Project.
func (r Runtime) enqueueDriftedIntent(ctx context.Context, scopeValue scope.IngestionScope, mat content.Materialization) error {
	if r.IntentWriter == nil {
		return errors.New("reducer intent writer is required when fingerprints changed")
	}
	intent := ReducerIntent{
		ScopeID:      mat.ScopeID,
		GenerationID: mat.GenerationID,
		Domain:       reducer.DomainCodeDrifted,
		EntityKey:    "code_drifted:" + mat.ScopeID,
		Reason:       "fingerprints published",
		SourceSystem: mat.SourceSystem,
		Payload:      map[string]any{"repo_id": mat.RepoID},
	}
	if _, err := r.IntentWriter.Enqueue(ctx, []ReducerIntent{intent}); err != nil {
		return fmt.Errorf("enqueue code drifted intent: %w", err)
	}
	if r.Instruments != nil {
		r.Instruments.ReducerIntentsEnqueued.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
		))
	}
	return nil
}

func (r Runtime) writeCanonicalProjection(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generationID string,
	inputFacts []facts.Envelope,
	mat canonical.CanonicalMaterialization,
) error {
	if mat.IsEmpty() {
		// Issue #7671: an empty materialization succeeds without writing any
		// node. That is legitimate for generations carrying no canonical rows,
		// but a file-bearing generation landing here means every file row was
		// dropped upstream — fatal decodes fail loudly and empty identities
		// quarantine, so anything else is a new silent path. Warn with the
		// input counts so an operator can tell the two apart without
		// recounting facts.
		if r.Logger != nil && len(inputFacts) > 0 {
			attrs := []any{
				slog.String("scope_id", scopeValue.ScopeID),
				slog.String("generation_id", generationID),
				slog.Int("fact_count", len(inputFacts)),
				slog.Int("file_fact_count", len(decode.FilterFileFacts(inputFacts))),
			}
			// File-bearing generations are the tripwire: every file row was
			// dropped upstream. Fact shapes with no file rows (unknown-kind
			// only, tombstone-only) are legitimate canonical-empty.
			if len(decode.FilterFileFacts(inputFacts)) > 0 {
				r.Logger.WarnContext(ctx, "projector canonical materialization empty with input facts; no graph nodes will be written", attrs...)
			} else {
				r.Logger.DebugContext(ctx, "projector canonical materialization empty with input facts; no graph nodes will be written", attrs...)
			}
		}
		if err := r.publishCanonicalGraphPhases(ctx, generationID, inputFacts); err != nil {
			return fmt.Errorf("publish canonical graph phases: %w", err)
		}
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

	if err := r.withPackageRegistryIdentityLocks(ctx, mat, func(lockCtx context.Context) error {
		return r.CanonicalWriter.Write(lockCtx, mat)
	}); err != nil {
		return fmt.Errorf("write canonical projection: %w", err)
	}
	if err := r.publishCanonicalGraphPhases(ctx, generationID, inputFacts); err != nil {
		return fmt.Errorf("publish canonical graph phases: %w", err)
	}

	if r.Instruments != nil {
		canonicalDur := time.Since(canonicalStart).Seconds()
		r.Instruments.CanonicalProjectionDuration.Record(ctx, canonicalDur, metric.WithAttributes(
			telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
		))
		r.Instruments.CanonicalWrites.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
		))
		r.Instruments.ProjectorStageDuration.Record(ctx, canonicalDur, metric.WithAttributes(
			telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
			attribute.String("stage", "canonical_write"),
		))
	}
	r.logRuntimeStage(
		ctx, scopeValue, generationID, "canonical_write", canonicalStart,
		"repository_count", canonicalRepositoryCount(mat),
		"directory_count", len(mat.Directories),
		"file_count", len(mat.Files),
		"entity_count", len(mat.Entities),
		"module_count", len(mat.Modules),
		"import_count", len(mat.Imports),
		"terraform_state_resource_count", len(mat.TerraformStateResources),
		"terraform_state_module_count", len(mat.TerraformStateModules),
		"terraform_state_output_count", len(mat.TerraformStateOutputs),
		"package_registry_package_count", len(mat.PackageRegistryPackages),
		"package_registry_version_count", len(mat.PackageRegistryVersions),
		"package_registry_dependency_count", len(mat.PackageRegistryDependencies),
	)

	return nil
}

func canonicalRepositoryCount(mat canonical.CanonicalMaterialization) int {
	if mat.Repository == nil {
		return 0
	}
	return 1
}
