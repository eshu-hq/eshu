package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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
	LoadSnapshot(ctx context.Context) (summary.Snapshot, error)
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
	ReplaceSources(ctx context.Context, repo string, sources []interproc.Source, updatedAt time.Time) error
}

// CodeFunctionGraphIDLoader loads the FunctionID->graph-uid map emitted for one
// scope generation.
type CodeFunctionGraphIDLoader interface {
	LoadCodeFunctionGraphIDs(
		ctx context.Context,
		scopeID string,
		generationID string,
	) (map[summary.FunctionID]string, error)
}

// CodeFunctionGraphIDWriter persists the FunctionID->graph-uid map. It is
// satisfied by postgres.FunctionGraphIDStore.
type CodeFunctionGraphIDWriter interface {
	UpsertGraphIDs(ctx context.Context, ids map[summary.FunctionID]string, updatedAt time.Time) error
}

// CodeFunctionSummaryMaterializationHandler persists one generation's function
// summaries: it loads the raw Effects, recomputes their content versions through
// a summary.Store, and upserts the resulting snapshot. The upsert is idempotent
// on FunctionID, so re-running a generation converges rather than duplicating.
// When the optional source and graph-id loader/writers are wired it also persists
// that generation's param-level taint sources and the FunctionID->uid map, which
// the cross-repo fixpoint needs alongside the summaries.
type CodeFunctionSummaryMaterializationHandler struct {
	Loader        CodeFunctionSummaryLoader
	Writer        CodeFunctionSummaryWriter
	SourceLoader  CodeFunctionSourceLoader
	SourceWriter  CodeFunctionSourceWriter
	GraphIDLoader CodeFunctionGraphIDLoader
	GraphIDWriter CodeFunctionGraphIDWriter
	Now           func() time.Time
	Instruments   *telemetry.Instruments
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

	current, err := h.Writer.LoadSnapshot(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load durable code function summary snapshot: %w", err)
	}
	store := summary.Load(current)
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
		for _, repo := range codeFunctionSourceRepos(effects, sources) {
			if err := h.SourceWriter.ReplaceSources(ctx, repo, codeFunctionSourcesForRepo(repo, sources), now); err != nil {
				return Result{}, fmt.Errorf("persist code function sources for repo %q: %w", repo, err)
			}
		}
		sourceCount = len(sources)
	}

	graphIDCount := 0
	if h.GraphIDLoader != nil && h.GraphIDWriter != nil {
		ids, err := h.GraphIDLoader.LoadCodeFunctionGraphIDs(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			return Result{}, fmt.Errorf("load code function graph ids: %w", err)
		}
		if err := h.GraphIDWriter.UpsertGraphIDs(ctx, ids, now); err != nil {
			return Result{}, fmt.Errorf("persist code function graph ids: %w", err)
		}
		graphIDCount = len(ids)
	}

	slog.Info(
		"code function summary persistence completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"function_count", len(snap.Functions),
		"source_count", sourceCount,
		"graph_id_count", graphIDCount,
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

func codeFunctionSourceRepos(
	effects map[summary.FunctionID]summary.Effects,
	sources []interproc.Source,
) []string {
	seen := make(map[string]struct{})
	for fnID := range effects {
		if repo := durableFunctionRepo(string(fnID)); repo != "" {
			seen[repo] = struct{}{}
		}
	}
	for _, src := range sources {
		if repo := durableFunctionRepo(string(src.Port.Func)); repo != "" {
			seen[repo] = struct{}{}
		}
	}
	repos := make([]string, 0, len(seen))
	for repo := range seen {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	return repos
}

func codeFunctionSourcesForRepo(repo string, sources []interproc.Source) []interproc.Source {
	var out []interproc.Source
	for _, src := range sources {
		if durableFunctionRepo(string(src.Port.Func)) == repo {
			out = append(out, src)
		}
	}
	return out
}

func durableFunctionRepo(functionID string) string {
	functionID = strings.TrimSpace(functionID)
	if idx := strings.Index(functionID, "\x1f"); idx >= 0 {
		return functionID[:idx]
	}
	return ""
}
