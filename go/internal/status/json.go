package status

import (
	"encoding/json"
	"time"

	"slices"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

// RenderJSON returns a stable machine-readable projection of the report.
func RenderJSON(report Report) ([]byte, error) {
	payload := struct {
		Version               string                     `json:"version"`
		AsOf                  string                     `json:"as_of"`
		Health                HealthSummary              `json:"health"`
		Coordinator           *coordinatorSnapshotJSON   `json:"coordinator,omitempty"`
		Flow                  []flowSummaryJSON          `json:"flow"`
		Queue                 queueJSON                  `json:"queue"`
		LatestFailure         *queueFailureJSON          `json:"latest_failure,omitempty"`
		RetryPolicies         []retryPolicyJSON          `json:"retry_policies"`
		ScopeActivity         scopeActivityJSON          `json:"scope_activity"`
		GenerationHistory     generationHistoryJSON      `json:"generation_history"`
		GenerationTransitions []generationTransitionJSON `json:"generation_transitions"`
		Scopes                map[string]int             `json:"scopes"`
		Generations           map[string]int             `json:"generations"`
		Stages                []StageSummary             `json:"stages"`
		Domains               []domainBacklogJSON        `json:"domains"`
		QueueBlockages        []queueBlockageJSON        `json:"queue_blockages"`
	}{
		Version:               buildinfo.AppVersion(),
		AsOf:                  report.AsOf.UTC().Format(time.RFC3339),
		Health:                report.Health,
		Coordinator:           coordinatorJSON(report.Coordinator),
		Flow:                  flowSummariesJSON(report.FlowSummaries),
		Queue:                 queueJSONFromReport(report.Queue),
		LatestFailure:         queueFailureJSONFromReport(report.LatestQueueFailure),
		RetryPolicies:         retryPoliciesJSON(report.RetryPolicies),
		ScopeActivity:         scopeActivityJSONFromReport(report.ScopeActivity),
		GenerationHistory:     generationHistoryJSONFromReport(report.GenerationHistory),
		GenerationTransitions: generationTransitionsJSON(report.GenerationTransitions),
		Scopes:                cloneCounts(report.ScopeTotals),
		Generations:           cloneCounts(report.GenerationTotals),
		Stages:                slices.Clone(report.StageSummaries),
		Domains:               domainBacklogsJSON(report.DomainBacklogs),
		QueueBlockages:        queueBlockagesJSON(report.QueueBlockages),
	}

	return json.MarshalIndent(payload, "", "  ")
}

type queueJSON struct {
	Total                       int     `json:"total"`
	Outstanding                 int     `json:"outstanding"`
	Pending                     int     `json:"pending"`
	InFlight                    int     `json:"in_flight"`
	Retrying                    int     `json:"retrying"`
	Succeeded                   int     `json:"succeeded"`
	Failed                      int     `json:"failed"`
	DeadLetter                  int     `json:"dead_letter"`
	OverdueClaims               int     `json:"overdue_claims"`
	OldestOutstandingAge        string  `json:"oldest_outstanding_age"`
	OldestOutstandingAgeSeconds float64 `json:"oldest_outstanding_age_seconds"`
}

type queueFailureJSON struct {
	Stage          string `json:"stage"`
	Domain         string `json:"domain"`
	Status         string `json:"status"`
	WorkItemID     string `json:"work_item_id,omitempty"`
	ScopeID        string `json:"scope_id,omitempty"`
	GenerationID   string `json:"generation_id,omitempty"`
	FailureClass   string `json:"failure_class"`
	FailureMessage string `json:"failure_message,omitempty"`
	FailureDetails string `json:"failure_details,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

type scopeActivityJSON struct {
	Active    int `json:"active"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

type generationHistoryJSON struct {
	Active     int `json:"active"`
	Pending    int `json:"pending"`
	Completed  int `json:"completed"`
	Superseded int `json:"superseded"`
	Failed     int `json:"failed"`
	Other      int `json:"other"`
}

type namedCountJSON struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type collectorInstanceJSON struct {
	InstanceID     string  `json:"instance_id"`
	CollectorKind  string  `json:"collector_kind"`
	Mode           string  `json:"mode"`
	Enabled        bool    `json:"enabled"`
	Bootstrap      bool    `json:"bootstrap"`
	ClaimsEnabled  bool    `json:"claims_enabled"`
	DisplayName    string  `json:"display_name,omitempty"`
	LastObservedAt string  `json:"last_observed_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeactivatedAt  *string `json:"deactivated_at,omitempty"`
}

type coordinatorSnapshotJSON struct {
	CollectorInstances   []collectorInstanceJSON `json:"collector_instances"`
	RunStatusCounts      []namedCountJSON        `json:"run_status_counts"`
	WorkItemStatusCounts []namedCountJSON        `json:"work_item_status_counts"`
	CompletenessCounts   []namedCountJSON        `json:"completeness_counts"`
	ActiveClaims         int                     `json:"active_claims"`
	OverdueClaims        int                     `json:"overdue_claims"`
	OldestPendingAge     string                  `json:"oldest_pending_age"`
	OldestPendingSeconds float64                 `json:"oldest_pending_age_seconds"`
}

type domainBacklogJSON struct {
	Domain           string  `json:"domain"`
	Outstanding      int     `json:"outstanding"`
	Retrying         int     `json:"retrying"`
	Failed           int     `json:"failed"`
	DeadLetter       int     `json:"dead_letter"`
	OldestAge        string  `json:"oldest_age"`
	OldestAgeSeconds float64 `json:"oldest_age_seconds"`
}

type queueBlockageJSON struct {
	Stage            string  `json:"stage"`
	Domain           string  `json:"domain"`
	ConflictDomain   string  `json:"conflict_domain"`
	ConflictKey      string  `json:"conflict_key"`
	Blocked          int     `json:"blocked"`
	OldestAge        string  `json:"oldest_age"`
	OldestAgeSeconds float64 `json:"oldest_age_seconds"`
}

func queueJSONFromReport(queue QueueSnapshot) queueJSON {
	return queueJSON{
		Total:                       queue.Total,
		Outstanding:                 queue.Outstanding,
		Pending:                     queue.Pending,
		InFlight:                    queue.InFlight,
		Retrying:                    queue.Retrying,
		Succeeded:                   queue.Succeeded,
		Failed:                      queue.Failed,
		DeadLetter:                  queue.DeadLetter,
		OverdueClaims:               queue.OverdueClaims,
		OldestOutstandingAge:        queue.OldestOutstandingAge.String(),
		OldestOutstandingAgeSeconds: queue.OldestOutstandingAge.Seconds(),
	}
}

func queueFailureJSONFromReport(snapshot *QueueFailureSnapshot) *queueFailureJSON {
	if snapshot == nil {
		return nil
	}

	return &queueFailureJSON{
		Stage:          snapshot.Stage,
		Domain:         snapshot.Domain,
		Status:         snapshot.Status,
		WorkItemID:     snapshot.WorkItemID,
		ScopeID:        snapshot.ScopeID,
		GenerationID:   snapshot.GenerationID,
		FailureClass:   snapshot.FailureClass,
		FailureMessage: snapshot.FailureMessage,
		FailureDetails: snapshot.FailureDetails,
		UpdatedAt:      nullableRFC3339Value(snapshot.UpdatedAt),
	}
}

func scopeActivityJSONFromReport(scopeActivity ScopeActivitySnapshot) scopeActivityJSON {
	return scopeActivityJSON(scopeActivity)
}

func domainBacklogsJSON(rows []DomainBacklog) []domainBacklogJSON {
	projected := make([]domainBacklogJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, domainBacklogJSON{
			Domain:           row.Domain,
			Outstanding:      row.Outstanding,
			Retrying:         row.Retrying,
			Failed:           row.Failed,
			DeadLetter:       row.DeadLetter,
			OldestAge:        row.OldestAge.String(),
			OldestAgeSeconds: row.OldestAge.Seconds(),
		})
	}

	return projected
}

func queueBlockagesJSON(rows []QueueBlockage) []queueBlockageJSON {
	projected := make([]queueBlockageJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, queueBlockageJSON{
			Stage:            row.Stage,
			Domain:           row.Domain,
			ConflictDomain:   row.ConflictDomain,
			ConflictKey:      row.ConflictKey,
			Blocked:          row.Blocked,
			OldestAge:        row.OldestAge.String(),
			OldestAgeSeconds: row.OldestAge.Seconds(),
		})
	}

	return projected
}

func coordinatorJSON(snapshot *CoordinatorSnapshot) *coordinatorSnapshotJSON {
	if snapshot == nil {
		return nil
	}

	instances := make([]collectorInstanceJSON, 0, len(snapshot.CollectorInstances))
	for _, instance := range snapshot.CollectorInstances {
		instances = append(instances, collectorInstanceJSON{
			InstanceID:     instance.InstanceID,
			CollectorKind:  instance.CollectorKind,
			Mode:           instance.Mode,
			Enabled:        instance.Enabled,
			Bootstrap:      instance.Bootstrap,
			ClaimsEnabled:  instance.ClaimsEnabled,
			DisplayName:    instance.DisplayName,
			LastObservedAt: instance.LastObservedAt.UTC().Format(time.RFC3339),
			UpdatedAt:      instance.UpdatedAt.UTC().Format(time.RFC3339),
			DeactivatedAt:  nullableRFC3339String(instance.DeactivatedAt),
		})
	}

	return &coordinatorSnapshotJSON{
		CollectorInstances:   instances,
		RunStatusCounts:      namedCountsJSON(snapshot.RunStatusCounts),
		WorkItemStatusCounts: namedCountsJSON(snapshot.WorkItemStatusCounts),
		CompletenessCounts:   namedCountsJSON(snapshot.CompletenessCounts),
		ActiveClaims:         snapshot.ActiveClaims,
		OverdueClaims:        snapshot.OverdueClaims,
		OldestPendingAge:     snapshot.OldestPendingAge.String(),
		OldestPendingSeconds: snapshot.OldestPendingAge.Seconds(),
	}
}

func namedCountsJSON(rows []NamedCount) []namedCountJSON {
	projected := make([]namedCountJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, namedCountJSON(row))
	}
	return projected
}

func nullableRFC3339String(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func nullableRFC3339Value(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
