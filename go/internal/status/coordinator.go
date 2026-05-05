package status

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// CollectorInstanceSummary captures the operator-visible durable shape of one
// configured collector runtime instance.
type CollectorInstanceSummary struct {
	InstanceID     string    `json:"instance_id"`
	CollectorKind  string    `json:"collector_kind"`
	Mode           string    `json:"mode"`
	Enabled        bool      `json:"enabled"`
	Bootstrap      bool      `json:"bootstrap"`
	ClaimsEnabled  bool      `json:"claims_enabled"`
	DisplayName    string    `json:"display_name,omitempty"`
	LastObservedAt time.Time `json:"last_observed_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	DeactivatedAt  time.Time `json:"deactivated_at,omitempty"`
}

// CoordinatorSnapshot captures additive workflow-coordinator state without
// redefining the platform health contract.
type CoordinatorSnapshot struct {
	CollectorInstances   []CollectorInstanceSummary `json:"collector_instances"`
	RunStatusCounts      []NamedCount               `json:"run_status_counts"`
	WorkItemStatusCounts []NamedCount               `json:"work_item_status_counts"`
	CompletenessCounts   []NamedCount               `json:"completeness_counts"`
	ActiveClaims         int                        `json:"active_claims"`
	OverdueClaims        int                        `json:"overdue_claims"`
	OldestPendingAge     time.Duration              `json:"oldest_pending_age"`
}

func cloneCoordinatorSnapshot(snapshot *CoordinatorSnapshot) *CoordinatorSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := &CoordinatorSnapshot{
		CollectorInstances:   slices.Clone(snapshot.CollectorInstances),
		RunStatusCounts:      slices.Clone(snapshot.RunStatusCounts),
		WorkItemStatusCounts: slices.Clone(snapshot.WorkItemStatusCounts),
		CompletenessCounts:   slices.Clone(snapshot.CompletenessCounts),
		ActiveClaims:         snapshot.ActiveClaims,
		OverdueClaims:        snapshot.OverdueClaims,
		OldestPendingAge:     snapshot.OldestPendingAge,
	}
	return cloned
}

func renderCoordinatorLines(snapshot *CoordinatorSnapshot) []string {
	if snapshot == nil {
		return nil
	}

	lines := []string{
		fmt.Sprintf(
			"Coordinator: instances=%d active_claims=%d overdue_claims=%d oldest_pending=%s",
			len(snapshot.CollectorInstances),
			snapshot.ActiveClaims,
			snapshot.OverdueClaims,
			snapshot.OldestPendingAge,
		),
	}
	if len(snapshot.RunStatusCounts) > 0 {
		lines = append(lines, fmt.Sprintf("Coordinator runs: %s", formatNamedTotals(toCountMap(snapshot.RunStatusCounts))))
	}
	if len(snapshot.WorkItemStatusCounts) > 0 {
		lines = append(lines, fmt.Sprintf("Coordinator work items: %s", formatNamedTotals(toCountMap(snapshot.WorkItemStatusCounts))))
	}
	if len(snapshot.CompletenessCounts) > 0 {
		lines = append(lines, fmt.Sprintf("Coordinator completeness: %s", formatNamedTotals(toCountMap(snapshot.CompletenessCounts))))
	}
	if len(snapshot.CollectorInstances) > 0 {
		lines = append(lines, "Collector instances:")
		for _, instance := range snapshot.CollectorInstances {
			line := fmt.Sprintf(
				"  %s kind=%s mode=%s enabled=%t bootstrap=%t claims_enabled=%t",
				instance.InstanceID,
				instance.CollectorKind,
				instance.Mode,
				instance.Enabled,
				instance.Bootstrap,
				instance.ClaimsEnabled,
			)
			if strings.TrimSpace(instance.DisplayName) != "" {
				line += fmt.Sprintf(" display_name=%s", instance.DisplayName)
			}
			lines = append(lines, line)
		}
	}
	return lines
}
