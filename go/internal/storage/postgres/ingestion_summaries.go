package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type functionSummaryCommitStats struct {
	Repos      int
	Recomputed int
}

func upsertFunctionSummariesForGeneration(
	ctx context.Context,
	db ExecQueryer,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	summaries []collector.ValueFlowSummarySnapshot,
	updatedAt time.Time,
) (functionSummaryCommitStats, error) {
	scopeRepo, err := functionSummaryScopeRepo(scopeValue)
	if err != nil {
		return functionSummaryCommitStats{}, err
	}
	updatesByRepo := make(map[string]map[summary.FunctionID]summary.Effects)
	if !generation.IsDelta {
		updatesByRepo[scopeRepo] = map[summary.FunctionID]summary.Effects{}
	}
	for _, row := range summaries {
		repo, err := functionSummaryRepo(row.FunctionID)
		if err != nil {
			return functionSummaryCommitStats{}, err
		}
		if repo != scopeRepo {
			return functionSummaryCommitStats{}, fmt.Errorf(
				"function summary %q belongs to repo %q outside scope repo %q",
				row.FunctionID,
				repo,
				scopeRepo,
			)
		}
		if updatesByRepo[repo] == nil {
			updatesByRepo[repo] = make(map[summary.FunctionID]summary.Effects)
		}
		updatesByRepo[repo][row.FunctionID] = row.Effects
	}
	if len(updatesByRepo) == 0 {
		return functionSummaryCommitStats{}, nil
	}

	store := NewFunctionSummaryStore(db)
	repos := make([]string, 0, len(updatesByRepo))
	for repo := range updatesByRepo {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	stats := functionSummaryCommitStats{Repos: len(repos)}
	for _, repo := range repos {
		summaryStore := summary.NewStore()
		if generation.IsDelta {
			existing, err := store.LoadRepoSnapshot(ctx, repo)
			if err != nil {
				return functionSummaryCommitStats{}, fmt.Errorf("load function summaries for repo %q: %w", repo, err)
			}
			summaryStore = summary.Load(existing)
		}
		recomputed := summaryStore.Upsert(updatesByRepo[repo])
		fullSnapshot := summaryStore.Snapshot()
		if generation.IsDelta {
			if len(recomputed) > 0 {
				snap := snapshotForFunctionIDs(fullSnapshot, recomputed)
				if err := store.UpsertSnapshot(ctx, snap, updatedAt); err != nil {
					return functionSummaryCommitStats{}, err
				}
			}
		} else if err := store.ReplaceRepoSnapshot(ctx, repo, fullSnapshot, updatedAt); err != nil {
			return functionSummaryCommitStats{}, err
		}
		if err := store.UpsertGenerationSnapshot(ctx, generation.GenerationID, fullSnapshot, updatedAt); err != nil {
			return functionSummaryCommitStats{}, err
		}
		stats.Recomputed += len(recomputed)
	}
	return stats, nil
}

func functionSummaryScopeRepo(scopeValue scope.IngestionScope) (string, error) {
	if repo := scopeValue.Metadata["repo_id"]; repo != "" {
		return repo, nil
	}
	if scopeValue.PartitionKey != "" {
		return scopeValue.PartitionKey, nil
	}
	return "", fmt.Errorf("function summary scope repo is required for scope %q", scopeValue.ScopeID)
}

func snapshotForFunctionIDs(snap summary.Snapshot, ids []summary.FunctionID) summary.Snapshot {
	wanted := make(map[summary.FunctionID]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	out := summary.Snapshot{Functions: make([]summary.SnapshotFunction, 0, len(ids))}
	for _, fn := range snap.Functions {
		if _, ok := wanted[fn.ID]; ok {
			out.Functions = append(out.Functions, fn)
		}
	}
	return out
}
