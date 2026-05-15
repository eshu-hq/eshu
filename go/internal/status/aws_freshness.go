package status

import (
	"fmt"
	"slices"
	"time"
)

// AWSFreshnessSnapshot captures aggregate EventBridge/AWS Config freshness
// trigger backlog state for the admin status surface.
type AWSFreshnessSnapshot struct {
	StatusCounts    []NamedCount
	OldestQueuedAge time.Duration
}

type awsFreshnessJSON struct {
	StatusCounts           []namedCountJSON `json:"status_counts"`
	OldestQueuedAge        string           `json:"oldest_queued_age"`
	OldestQueuedAgeSeconds float64          `json:"oldest_queued_age_seconds"`
}

func cloneAWSFreshnessSnapshot(snapshot AWSFreshnessSnapshot) AWSFreshnessSnapshot {
	return AWSFreshnessSnapshot{
		StatusCounts:    slices.Clone(snapshot.StatusCounts),
		OldestQueuedAge: snapshot.OldestQueuedAge,
	}
}

func renderAWSFreshnessLines(snapshot AWSFreshnessSnapshot) []string {
	if len(snapshot.StatusCounts) == 0 && snapshot.OldestQueuedAge == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"AWS freshness: queued=%d claimed=%d handed_off=%d failed=%d oldest_queued=%s",
		awsFreshnessCount(snapshot.StatusCounts, "queued"),
		awsFreshnessCount(snapshot.StatusCounts, "claimed"),
		awsFreshnessCount(snapshot.StatusCounts, "handed_off"),
		awsFreshnessCount(snapshot.StatusCounts, "failed"),
		snapshot.OldestQueuedAge,
	)}
}

func awsFreshnessJSONFromReport(snapshot AWSFreshnessSnapshot) *awsFreshnessJSON {
	if len(snapshot.StatusCounts) == 0 && snapshot.OldestQueuedAge == 0 {
		return nil
	}
	counts := make([]namedCountJSON, 0, len(snapshot.StatusCounts))
	for _, count := range snapshot.StatusCounts {
		counts = append(counts, namedCountJSON(count))
	}
	return &awsFreshnessJSON{
		StatusCounts:           counts,
		OldestQueuedAge:        snapshot.OldestQueuedAge.String(),
		OldestQueuedAgeSeconds: snapshot.OldestQueuedAge.Seconds(),
	}
}

func awsFreshnessCount(counts []NamedCount, name string) int {
	for _, count := range counts {
		if count.Name == name {
			return count.Count
		}
	}
	return 0
}
