package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

func codeFunctionSummaryDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCodeFunctionSummary,
		Summary: "persist durable value-flow function summaries for cross-repo composition",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "code_function_summary",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// CodeFunctionSummaryLoader loads the value-flow Effects emitted for one scope
// generation, keyed by the durable FunctionID.
type CodeFunctionSummaryLoader interface {
	LoadCodeFunctionSummaryEffects(
		ctx context.Context,
		scopeID string,
		generationID string,
	) (map[summary.FunctionID]summary.Effects, error)
}

// CodeFunctionSummaryWriter persists a resolved function-summary snapshot to the
// durable store. It is satisfied by postgres.FunctionSummaryStore.
type CodeFunctionSummaryWriter interface {
	UpsertSnapshot(ctx context.Context, snap summary.Snapshot, updatedAt time.Time) error
}

// CodeFunctionSourceLoader loads the param-level value-flow taint sources emitted
// for one scope generation as interproc source ports.
type CodeFunctionSourceLoader interface {
	LoadCodeFunctionSources(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]interproc.Source, error)
}

// CodeFunctionSourceWriter persists the param-level taint sources to the durable
// store. It is satisfied by postgres.FunctionSourceStore.
type CodeFunctionSourceWriter interface {
	UpsertSources(ctx context.Context, sources []interproc.Source, updatedAt time.Time) error
}

// CodeFunctionSummaryMaterializationHandler persists one generation's function
// summaries: it loads the raw Effects, recomputes their content versions through
// a summary.Store, and upserts the resulting snapshot. The upsert is idempotent
// on FunctionID, so re-running a generation converges rather than duplicating.
// When the optional source loader/writer are wired it also persists that
// generation's param-level taint sources, which the cross-repo fixpoint needs as
// entry points alongside the summaries.
type CodeFunctionSummaryMaterializationHandler struct {
	Loader       CodeFunctionSummaryLoader
	Writer       CodeFunctionSummaryWriter
	SourceLoader CodeFunctionSourceLoader
	SourceWriter CodeFunctionSourceWriter
	Now          func() time.Time
	Instruments  *telemetry.Instruments
}

// Handle executes one function-summary persistence intent.
func (h CodeFunctionSummaryMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCodeFunctionSummary {
		return Result{}, fmt.Errorf("code function summary handler does not accept domain %q", intent.Domain)
	}
	if h.Loader == nil {
		return Result{}, fmt.Errorf("code function summary loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("code function summary writer is required")
	}

	effects, err := h.Loader.LoadCodeFunctionSummaryEffects(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load code function summaries: %w", err)
	}

	store := summary.NewStore()
	store.Upsert(effects)
	snap := store.Snapshot()

	now := h.now()
	if err := h.Writer.UpsertSnapshot(ctx, snap, now); err != nil {
		return Result{}, fmt.Errorf("persist code function summaries: %w", err)
	}

	sourceCount := 0
	if h.SourceLoader != nil && h.SourceWriter != nil {
		sources, err := h.SourceLoader.LoadCodeFunctionSources(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			return Result{}, fmt.Errorf("load code function sources: %w", err)
		}
		if err := h.SourceWriter.UpsertSources(ctx, sources, now); err != nil {
			return Result{}, fmt.Errorf("persist code function sources: %w", err)
		}
		sourceCount = len(sources)
	}

	slog.Info(
		"code function summary persistence completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"function_count", len(snap.Functions),
		"source_count", sourceCount,
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainCodeFunctionSummary,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("persisted %d function summary row(s)", len(snap.Functions)),
		CanonicalWrites: len(snap.Functions),
	}, nil
}

// now returns the handler clock, defaulting to time.Now when unset.
func (h CodeFunctionSummaryMaterializationHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}
