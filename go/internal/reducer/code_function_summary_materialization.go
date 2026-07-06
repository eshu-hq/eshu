// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

// CodeFunctionSummaryLoader loads the raw code_function_summary fact envelopes
// for one scope generation. The handler decodes them through the typed
// contracts seam (ExtractCodeFunctionSummaryEffectsWithQuarantine /
// ExtractCodeFunctionGraphIDsWithQuarantine) so a fact missing its required
// function_id dead-letters as an input_invalid quarantine rather than being
// silently dropped (Contract System v1 Wave 4f S2, issue #4754). The
// FunctionID->Effects and FunctionID->graph-uid views both derive from these
// same envelopes.
type CodeFunctionSummaryLoader interface {
	LoadCodeFunctionSummaryFacts(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]facts.Envelope, error)
}

// CodeFunctionSummaryWriter persists a resolved function-summary snapshot to the
// durable store. It is satisfied by postgres.FunctionSummaryStore.
type CodeFunctionSummaryWriter interface {
	LoadSnapshot(ctx context.Context) (summary.Snapshot, error)
	UpsertSnapshot(ctx context.Context, snap summary.Snapshot, updatedAt time.Time) error
	ReplaceSnapshot(ctx context.Context, repo string, snap summary.Snapshot, updatedAt time.Time) error
}

// CodeFunctionSourceLoader loads the raw code_function_source fact envelopes
// for one scope generation. The handler decodes them through the typed
// contracts seam (ExtractCodeFunctionSourcesWithQuarantine) so a fact missing
// a required function_id/kind dead-letters as an input_invalid quarantine.
type CodeFunctionSourceLoader interface {
	LoadCodeFunctionSourceFacts(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]facts.Envelope, error)
}

// CodeFunctionSourceWriter persists the param-level taint sources to the durable
// store. It is satisfied by postgres.FunctionSourceStore.
type CodeFunctionSourceWriter interface {
	ReplaceSources(ctx context.Context, repo string, sources []interproc.Source, updatedAt time.Time) error
}

// CodeFunctionGraphIDLoader loads the raw code_function_summary fact envelopes
// for one scope generation (the same facts CodeFunctionSummaryLoader reads);
// the handler derives the FunctionID->graph-uid map from them through the
// typed contracts seam. It stays a distinct interface so the graph-id store
// wiring and its readiness gate are independent of the summary store's.
type CodeFunctionGraphIDLoader interface {
	LoadCodeFunctionGraphIDFacts(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]facts.Envelope, error)
}

// CodeFunctionGraphIDWriter persists the FunctionID->graph-uid map. It is
// satisfied by postgres.FunctionGraphIDStore.
type CodeFunctionGraphIDWriter interface {
	ReplaceGraphIDs(ctx context.Context, repo string, ids map[summary.FunctionID]string, updatedAt time.Time) error
}

// ValueFlowFixpointProjector projects durable cross-repo value-flow findings
// after summaries, sources, and graph ids have been persisted.
type ValueFlowFixpointProjector interface {
	ProjectValueFlowFixpointEvidence(ctx context.Context, scopeID, generationID string) (ValueFlowFixpointProjectionResult, error)
}

// CodeFunctionSummaryMaterializationHandler persists one generation's function
// summaries: it loads the raw Effects, recomputes their content versions through
// a summary.Store, and upserts the resulting snapshot. The upsert is idempotent
// on FunctionID, so re-running a generation converges rather than duplicating.
// When the optional source and graph-id loader/writers are wired it also persists
// that generation's param-level taint sources and the FunctionID->uid map, which
// the cross-repo fixpoint needs alongside the summaries. When the optional
// fixpoint projector is wired it runs after those durable writes complete, so
// graph projection cannot race ahead of persistence.
type CodeFunctionSummaryMaterializationHandler struct {
	Loader                  CodeFunctionSummaryLoader
	Writer                  CodeFunctionSummaryWriter
	SourceLoader            CodeFunctionSourceLoader
	SourceWriter            CodeFunctionSourceWriter
	GraphIDLoader           CodeFunctionGraphIDLoader
	GraphIDWriter           CodeFunctionGraphIDWriter
	ValueFlowFixpointWriter ValueFlowFixpointProjector
	Now                     func() time.Time
	Instruments             *telemetry.Instruments
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

	summaryFacts, err := h.Loader.LoadCodeFunctionSummaryFacts(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load code function summaries: %w", err)
	}
	effects, summaryQuarantined, err := ExtractCodeFunctionSummaryEffectsWithQuarantine(summaryFacts)
	if err != nil {
		return Result{}, fmt.Errorf("decode code function summaries: %w", err)
	}
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainCodeFunctionSummary, intent.ScopeID, intent.GenerationID, summaryQuarantined)

	current, err := h.Writer.LoadSnapshot(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load durable code function summary snapshot: %w", err)
	}
	fullSnapshot, repo := codeFunctionSummaryFullSnapshot(intent)
	if fullSnapshot && repo == "" {
		return Result{}, fmt.Errorf("code function summary full snapshot repo_id is required")
	}
	if fullSnapshot {
		for id := range effects {
			if got := durableFunctionRepo(string(id)); got != repo {
				return Result{}, fmt.Errorf("code function summary repo %q does not match full snapshot repo %q", got, repo)
			}
		}
	}
	if fullSnapshot {
		current = codeFunctionSummarySnapshotWithoutRepo(current, repo)
	}
	store := summary.Load(current)
	store.Upsert(effects)
	snap := store.Snapshot()

	now := h.now()
	persistedFunctionCount := len(snap.Functions)
	if fullSnapshot {
		repoSnap := codeFunctionSummarySnapshotForRepo(snap, repo)
		if err := h.Writer.ReplaceSnapshot(ctx, repo, repoSnap, now); err != nil {
			return Result{}, fmt.Errorf("replace code function summaries for repo %q: %w", repo, err)
		}
		persistedFunctionCount = len(repoSnap.Functions)
	} else {
		if err := h.Writer.UpsertSnapshot(ctx, snap, now); err != nil {
			return Result{}, fmt.Errorf("persist code function summaries: %w", err)
		}
	}

	sourceCount := 0
	if h.SourceLoader != nil && h.SourceWriter != nil {
		sourceFacts, err := h.SourceLoader.LoadCodeFunctionSourceFacts(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			return Result{}, fmt.Errorf("load code function sources: %w", err)
		}
		sources, sourceQuarantined, err := ExtractCodeFunctionSourcesWithQuarantine(sourceFacts)
		if err != nil {
			return Result{}, fmt.Errorf("decode code function sources: %w", err)
		}
		inputInvalidCount += recordQuarantinedFacts(ctx, h.Instruments, DomainCodeFunctionSummary, intent.ScopeID, intent.GenerationID, sourceQuarantined)
		for _, repo := range codeFunctionSourceRepos(effects, sources, codeFunctionSummaryCompanionRepo(fullSnapshot, repo)) {
			if err := h.SourceWriter.ReplaceSources(ctx, repo, codeFunctionSourcesForRepo(repo, sources), now); err != nil {
				return Result{}, fmt.Errorf("persist code function sources for repo %q: %w", repo, err)
			}
		}
		sourceCount = len(sources)
	}

	graphIDCount := 0
	if h.GraphIDLoader != nil && h.GraphIDWriter != nil {
		graphIDFacts, err := h.GraphIDLoader.LoadCodeFunctionGraphIDFacts(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			return Result{}, fmt.Errorf("load code function graph ids: %w", err)
		}
		// The graph-id view reads the SAME code_function_summary facts as the
		// summary-effects view already quarantined above, so its quarantines
		// are discarded here to avoid double-counting one malformed fact on
		// the input_invalid counter; a residual FATAL decode error still
		// propagates and fails the intent.
		ids, _, err := ExtractCodeFunctionGraphIDsWithQuarantine(graphIDFacts)
		if err != nil {
			return Result{}, fmt.Errorf("decode code function graph ids: %w", err)
		}
		for _, repo := range codeFunctionGraphIDRepos(effects, ids, codeFunctionSummaryCompanionRepo(fullSnapshot, repo)) {
			if err := h.GraphIDWriter.ReplaceGraphIDs(ctx, repo, codeFunctionGraphIDsForRepo(repo, ids), now); err != nil {
				return Result{}, fmt.Errorf("persist code function graph ids for repo %q: %w", repo, err)
			}
		}
		graphIDCount = len(ids)
	}

	fixpoint := ValueFlowFixpointProjectionResult{}
	if h.ValueFlowFixpointWriter != nil {
		var err error
		fixpoint, err = h.ValueFlowFixpointWriter.ProjectValueFlowFixpointEvidence(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			return Result{}, fmt.Errorf("project value-flow fixpoint evidence: %w", err)
		}
	}

	slog.Info(
		"code function summary persistence completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"repo_id", repo,
		"full_snapshot", fullSnapshot,
		"function_count", persistedFunctionCount,
		"source_count", sourceCount,
		"graph_id_count", graphIDCount,
		"input_invalid_facts", inputInvalidCount,
		"fixpoint_finding_count", fixpoint.FindingCount,
		"fixpoint_graph_rows", fixpoint.GraphRows,
		"fixpoint_unresolved_endpoint_count", fixpoint.UnresolvedEndpointCount,
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainCodeFunctionSummary,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"persisted %d function summary row(s), projected %d fixpoint edge(s)",
			persistedFunctionCount,
			fixpoint.GraphRows,
		),
		CanonicalWrites: persistedFunctionCount + fixpoint.GraphRows,
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
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
	requiredRepo string,
) []string {
	seen := make(map[string]struct{})
	if requiredRepo != "" {
		seen[requiredRepo] = struct{}{}
	}
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

func codeFunctionGraphIDRepos(
	effects map[summary.FunctionID]summary.Effects,
	ids map[summary.FunctionID]string,
	requiredRepo string,
) []string {
	seen := make(map[string]struct{})
	if requiredRepo != "" {
		seen[requiredRepo] = struct{}{}
	}
	for fnID := range effects {
		if repo := durableFunctionRepo(string(fnID)); repo != "" {
			seen[repo] = struct{}{}
		}
	}
	for fnID := range ids {
		if repo := durableFunctionRepo(string(fnID)); repo != "" {
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

func codeFunctionGraphIDsForRepo(repo string, ids map[summary.FunctionID]string) map[summary.FunctionID]string {
	out := make(map[summary.FunctionID]string)
	for fnID, uid := range ids {
		if durableFunctionRepo(string(fnID)) == repo {
			out[fnID] = uid
		}
	}
	return out
}

func codeFunctionSummaryCompanionRepo(fullSnapshot bool, repo string) string {
	if !fullSnapshot {
		return ""
	}
	return repo
}

func codeFunctionSummaryFullSnapshot(intent Intent) (bool, string) {
	fullSnapshot, _ := intent.Payload["full_snapshot"].(bool)
	repo, _ := intent.Payload["repo_id"].(string)
	return fullSnapshot, strings.TrimSpace(repo)
}

func codeFunctionSummarySnapshotWithoutRepo(snap summary.Snapshot, repo string) summary.Snapshot {
	if repo == "" || len(snap.Functions) == 0 {
		return snap
	}
	out := summary.Snapshot{Functions: make([]summary.SnapshotFunction, 0, len(snap.Functions))}
	for _, fn := range snap.Functions {
		if durableFunctionRepo(string(fn.ID)) == repo {
			continue
		}
		out.Functions = append(out.Functions, fn)
	}
	return out
}

func codeFunctionSummarySnapshotForRepo(snap summary.Snapshot, repo string) summary.Snapshot {
	out := summary.Snapshot{}
	if repo == "" || len(snap.Functions) == 0 {
		return out
	}
	for _, fn := range snap.Functions {
		if durableFunctionRepo(string(fn.ID)) == repo {
			out.Functions = append(out.Functions, fn)
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
