// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordFluxCrossRepoURLResolutionStats emits eshu_dp_flux_cross_repo_url_resolution_total
// once per non-zero outcome for one batch's
// relationships.DiscoverEvidenceWithStats tally (issue #5483 C2). It is a
// no-op when instruments or the counter are absent, so callers that never
// wire Instruments (most unit tests) never panic. It is called from the
// Postgres ingestion commit path (CommitScopeGeneration's per-batch
// callback), the sole caller that discovers Flux cross-repo evidence, BEFORE
// the batch's evidence-empty early return -- an unresolved/ambiguous/self
// outcome tallies even on a batch that emits zero evidence facts.
func recordFluxCrossRepoURLResolutionStats(
	ctx context.Context,
	instruments *telemetry.Instruments,
	stats relationships.FluxCrossRepoURLResolutionStats,
) {
	if instruments == nil || instruments.FluxCrossRepoURLResolution == nil {
		return
	}
	counter := instruments.FluxCrossRepoURLResolution
	recordFluxCrossRepoURLResolutionOutcome(ctx, counter, relationships.FluxCrossRepoURLResolutionOutcomeLinked, stats.Linked)
	recordFluxCrossRepoURLResolutionOutcome(ctx, counter, relationships.FluxCrossRepoURLResolutionOutcomeUnresolved, stats.Unresolved)
	recordFluxCrossRepoURLResolutionOutcome(ctx, counter, relationships.FluxCrossRepoURLResolutionOutcomeAmbiguous, stats.Ambiguous)
	recordFluxCrossRepoURLResolutionOutcome(ctx, counter, relationships.FluxCrossRepoURLResolutionOutcomeSelf, stats.Self)
}

func recordFluxCrossRepoURLResolutionOutcome(
	ctx context.Context,
	counter metric.Int64Counter,
	outcome string,
	count int,
) {
	if count <= 0 {
		return
	}
	counter.Add(ctx, int64(count), metric.WithAttributes(telemetry.AttrOutcome(outcome)))
}

// recordArgoCDApplicationSetTemplateSourceStats emits
// eshu_dp_argocd_applicationset_template_source_total once per non-zero outcome
// for one batch's relationships.DiscoverEvidenceWithStats tally (issue #7767),
// following the Flux cross-repo precedent. The tally counts distinct facts, not
// every considered url (see relationships.ApplicationSetTemplateSourceStats).
// It is a no-op when instruments or the counter are absent, so callers that
// never wire Instruments (most unit tests) never panic. It is called from the
// Postgres ingestion commit path (CommitScopeGeneration's per-batch callback)
// BEFORE the batch's evidence-empty early return, so a skipped_* outcome
// tallies even on a batch that emits zero evidence facts.
func recordArgoCDApplicationSetTemplateSourceStats(
	ctx context.Context,
	instruments *telemetry.Instruments,
	stats relationships.ApplicationSetTemplateSourceStats,
) {
	if instruments == nil || instruments.ArgoCDApplicationSetTemplateSource == nil {
		return
	}
	counter := instruments.ArgoCDApplicationSetTemplateSource
	recordApplicationSetTemplateSourceOutcome(ctx, counter, relationships.ApplicationSetTemplateSourceOutcomeDeploySource, stats.DeploySource)
	recordApplicationSetTemplateSourceOutcome(ctx, counter, relationships.ApplicationSetTemplateSourceOutcomeSelfReference, stats.SelfReference)
	recordApplicationSetTemplateSourceOutcome(ctx, counter, relationships.ApplicationSetTemplateSourceOutcomeSkippedControlRepo, stats.SkippedControlRepo)
	recordApplicationSetTemplateSourceOutcome(ctx, counter, relationships.ApplicationSetTemplateSourceOutcomeSkippedTemplatedDestination, stats.SkippedTemplatedDestination)
}

func recordApplicationSetTemplateSourceOutcome(
	ctx context.Context,
	counter metric.Int64Counter,
	outcome string,
	count int,
) {
	if count <= 0 {
		return
	}
	counter.Add(ctx, int64(count), metric.WithAttributes(telemetry.AttrOutcome(outcome)))
}
