package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

type functionSummaryCommitStats struct {
	Repos      int
	Recomputed int
}

func upsertFunctionSummariesForGeneration(
	ctx context.Context,
	db ExecQueryer,
	summaries []collector.ValueFlowSummarySnapshot,
	updatedAt time.Time,
) (functionSummaryCommitStats, error) {
	updatesByRepo := make(map[string]map[summary.FunctionID]summary.Effects)
	for _, row := range summaries {
		repo, err := functionSummaryRepo(row.FunctionID)
		if err != nil {
			return functionSummaryCommitStats{}, err
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
		existing, err := store.LoadRepoSnapshot(ctx, repo)
		if err != nil {
			return functionSummaryCommitStats{}, fmt.Errorf("load function summaries for repo %q: %w", repo, err)
		}
		summaryStore := summary.Load(existing)
		recomputed := summaryStore.Upsert(updatesByRepo[repo])
		if len(recomputed) == 0 {
			continue
		}
		snap := snapshotForFunctionIDs(summaryStore.Snapshot(), recomputed)
		if err := store.UpsertSnapshot(ctx, snap, updatedAt); err != nil {
			return functionSummaryCommitStats{}, err
		}
		stats.Recomputed += len(recomputed)
	}
	return stats, nil
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
