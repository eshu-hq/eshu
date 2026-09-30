// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eshusearch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	"go.opentelemetry.io/otel/metric"
)

// errGenerationSuperseded is the sentinel the page callback returns when the
// generation check reports the intent's generation is no longer the scope's
// active one. Handle detects it with errors.Is on the loader's return so it does
// not depend on the loader passing the callback error through unwrapped.
var errGenerationSuperseded = errors.New("eshu search document generation superseded")

// Abandon phases are the closed label set of the superseded counter and the
// phase log field: the check that noticed the supersede ran before a page
// callback or before Finalize.
const (
	searchDocumentPhasePage     = "page"
	searchDocumentPhaseFinalize = "finalize"
)

// searchDocumentWriteProgress counts what the streaming write actually landed,
// so an abandoned item reports the pages and documents it wrote rather than the
// documents it would have written.
type searchDocumentWriteProgress struct {
	pages     int
	documents int
}

// requireCurrentGeneration runs the generation check for the intent. It returns
// errGenerationSuperseded when the generation is superseded and wraps any check
// error unchanged, so a lookup failure (including a not-yet-active generation)
// takes the caller's stream-error path and fails closed instead of being read as
// either "current" or "superseded".
func (h EshuSearchDocumentHandler) requireCurrentGeneration(
	ctx context.Context,
	intent reducercontract.Intent,
) error {
	current, err := h.GenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return fmt.Errorf("check eshu search document generation: %w", err)
	}
	if !current {
		return errGenerationSuperseded
	}
	return nil
}

// abandonSuperseded ends a superseded intent without Cancel or Finalize and
// reports it as reducercontract.ResultStatusSuperseded, which the queue acks
// succeeded like any other superseded reducer intent (issue #7458).
//
// The rows already written are deliberately not cancelled. Cancel is the
// empty-keep-set retire: the same DELETE over fact_records and
// eshu_search_index_documents that measured about 91 s per large cycle, i.e. the
// cost this abandon removes. It is also unnecessary. Every search-document row
// is keyed by (scope_id, generation_id), and every reader joins the scope's
// active generation, so a superseded generation's rows are invisible; retention
// prunes them by cascade from scope_generations exactly as it prunes a
// superseded generation that finished before the newer one activated. The
// projection-state row stays 'building': MarkFailed would no-op against a
// non-active generation, and every consumer of that row is active-generation
// scoped. The abandoned pages are not counted in eshu_dp_canonical_writes_total
// because recordCycle only runs for a finalized projection.
func (h EshuSearchDocumentHandler) abandonSuperseded(
	ctx context.Context,
	intent reducercontract.Intent,
	phase string,
	progress searchDocumentWriteProgress,
	startedAt time.Time,
) reducercontract.Result {
	duration := time.Since(startedAt).Seconds()
	if h.Instruments != nil && h.Instruments.SearchDocumentGenerationSuperseded != nil {
		// The reducer context may already be cancelled by the time an operator
		// looks at the counter; keep the increment independent of it.
		h.Instruments.SearchDocumentGenerationSuperseded.Add(
			context.WithoutCancel(ctx), 1,
			metric.WithAttributes(telemetry.AttrPhase(phase)),
		)
	}
	if h.Logger != nil {
		h.Logger.InfoContext(
			ctx, "eshu search document projection abandoned: generation superseded",
			log.ScopeID(intent.ScopeID),
			log.GenerationID(intent.GenerationID),
			log.Domain(string(DomainEshuSearchDocument)),
			slog.String(telemetry.MetricDimensionPhase, phase),
			slog.Int("pages_written", progress.pages),
			slog.Int("documents_written", progress.documents),
			slog.Float64("duration_seconds", duration),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   reducercontract.ResultStatusSuperseded,
		EvidenceSummary: fmt.Sprintf(
			"eshu search document projection abandoned: generation superseded phase=%s pages_written=%d documents_written=%d",
			phase, progress.pages, progress.documents,
		),
		CanonicalWrites: progress.documents,
		CompletedAt:     time.Now(),
	}
}
