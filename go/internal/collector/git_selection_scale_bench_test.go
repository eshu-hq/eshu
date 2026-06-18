package collector

import (
	"fmt"
	"math"
	"testing"
)

func TestRepositoryShardScaleSummaryCoversEveryRepositoryOnce(t *testing.T) {
	t.Parallel()

	summary := repositoryShardScaleSummary(10_000, 8)
	if got, want := summary.totalSelected, 10_000; got != want {
		t.Fatalf("total selected = %d, want %d", got, want)
	}
	if got, want := summary.duplicateSelections, 0; got != want {
		t.Fatalf("duplicate selections = %d, want %d", got, want)
	}
	if got, want := summary.missingSelections, 0; got != want {
		t.Fatalf("missing selections = %d, want %d", got, want)
	}
	if spread := summary.maxShardSize - summary.minShardSize; spread > 100 {
		t.Fatalf("shard size spread = %d, want <= 100; counts=%v", spread, summary.shardSizes)
	}
}

func BenchmarkRepositoryShardSelectionScale(b *testing.B) {
	for _, repoCount := range []int{1_000, 10_000} {
		for _, shardCount := range []int{1, 4, 8} {
			b.Run(fmt.Sprintf("repos_%d/shards_%d", repoCount, shardCount), func(b *testing.B) {
				b.ReportAllocs()

				var summary repositoryShardScaleReport
				for i := 0; i < b.N; i++ {
					summary = repositoryShardScaleSummary(repoCount, shardCount)
				}
				if summary.totalSelected != repoCount || summary.duplicateSelections != 0 || summary.missingSelections != 0 {
					b.Fatalf("summary = %#v, want complete single coverage for %d repos", summary, repoCount)
				}
				b.ReportMetric(float64(summary.maxShardSize-summary.minShardSize), "repos_spread")
			})
		}
	}
}

type repositoryShardScaleReport struct {
	shardSizes          []int
	totalSelected       int
	duplicateSelections int
	missingSelections   int
	minShardSize        int
	maxShardSize        int
}

func repositoryShardScaleSummary(repoCount int, shardCount int) repositoryShardScaleReport {
	repositoryIDs := syntheticRepositoryIDs(repoCount)
	seen := make(map[string]int, repoCount)
	report := repositoryShardScaleReport{
		shardSizes:   make([]int, 0, shardCount),
		minShardSize: math.MaxInt,
	}

	for shardIndex := 0; shardIndex < shardCount; shardIndex++ {
		selected := filterRepositoryIDsByShard(repositoryIDs, RepoSyncConfig{
			RepoShardCount: shardCount,
			RepoShardIndex: shardIndex,
		})
		report.shardSizes = append(report.shardSizes, len(selected))
		report.totalSelected += len(selected)
		if len(selected) < report.minShardSize {
			report.minShardSize = len(selected)
		}
		if len(selected) > report.maxShardSize {
			report.maxShardSize = len(selected)
		}
		for _, repositoryID := range selected {
			seen[repositoryID]++
		}
	}

	if shardCount == 0 {
		report.minShardSize = 0
	}
	for _, count := range seen {
		if count > 1 {
			report.duplicateSelections += count - 1
		}
	}
	report.missingSelections = repoCount - len(seen)
	return report
}

func syntheticRepositoryIDs(repoCount int) []string {
	repositoryIDs := make([]string, 0, repoCount)
	for i := 0; i < repoCount; i++ {
		repositoryIDs = append(repositoryIDs, fmt.Sprintf("github.com/example/repo-%05d", i))
	}
	return repositoryIDs
}
