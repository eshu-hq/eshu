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
		RegistryCollectors    []registryCollectorJSON    `json:"registry_collectors,omitempty"`
		AWSCloudScans         []awsCloudScanJSON         `json:"aws_cloud_scans,omitempty"`
		ScopeActivity         scopeActivityJSON          `json:"scope_activity"`
		GenerationHistory     generationHistoryJSON      `json:"generation_history"`
		GenerationTransitions []generationTransitionJSON `json:"generation_transitions"`
		Scopes                map[string]int             `json:"scopes"`
		Generations           map[string]int             `json:"generations"`
		Stages                []StageSummary             `json:"stages"`
		Domains               []domainBacklogJSON        `json:"domains"`
		QueueBlockages        []queueBlockageJSON        `json:"queue_blockages"`
		TerraformState        *terraformStateJSON        `json:"terraform_state,omitempty"`
	}{
		Version:               buildinfo.AppVersion(),
		AsOf:                  report.AsOf.UTC().Format(time.RFC3339),
		Health:                report.Health,
		Coordinator:           coordinatorJSON(report.Coordinator),
		Flow:                  flowSummariesJSON(report.FlowSummaries),
		Queue:                 queueJSONFromReport(report.Queue),
		LatestFailure:         queueFailureJSONFromReport(report.LatestQueueFailure),
		RetryPolicies:         retryPoliciesJSON(report.RetryPolicies),
		RegistryCollectors:    registryCollectorsJSON(report.RegistryCollectors),
		AWSCloudScans:         awsCloudScansJSON(report.AWSCloudScans),
		ScopeActivity:         scopeActivityJSONFromReport(report.ScopeActivity),
		GenerationHistory:     generationHistoryJSONFromReport(report.GenerationHistory),
		GenerationTransitions: generationTransitionsJSON(report.GenerationTransitions),
		Scopes:                cloneCounts(report.ScopeTotals),
		Generations:           cloneCounts(report.GenerationTotals),
		Stages:                slices.Clone(report.StageSummaries),
		Domains:               domainBacklogsJSON(report.DomainBacklogs),
		QueueBlockages:        queueBlockagesJSON(report.QueueBlockages),
		TerraformState:        terraformStateReportJSON(report.TerraformState),
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

type registryCollectorJSON struct {
	CollectorKind              string           `json:"collector_kind"`
	ConfiguredInstances        int              `json:"configured_instances"`
	ActiveScopes               int              `json:"active_scopes"`
	RecentCompletedGenerations int              `json:"recent_completed_generations"`
	LastCompletedAt            string           `json:"last_completed_at,omitempty"`
	RetryableFailures          int              `json:"retryable_failures"`
	TerminalFailures           int              `json:"terminal_failures"`
	FailureClassCounts         []namedCountJSON `json:"failure_class_counts,omitempty"`
}

type awsCloudScanJSON struct {
	CollectorInstanceID string `json:"collector_instance_id"`
	AccountID           string `json:"account_id"`
	Region              string `json:"region"`
	ServiceKind         string `json:"service_kind"`
	Status              string `json:"status"`
	CommitStatus        string `json:"commit_status"`
	FailureClass        string `json:"failure_class,omitempty"`
	FailureMessage      string `json:"failure_message,omitempty"`
	APICallCount        int    `json:"api_call_count"`
	ThrottleCount       int    `json:"throttle_count"`
	WarningCount        int    `json:"warning_count"`
	ResourceCount       int    `json:"resource_count"`
	RelationshipCount   int    `json:"relationship_count"`
	TagObservationCount int    `json:"tag_observation_count"`
	BudgetExhausted     bool   `json:"budget_exhausted"`
	CredentialFailed    bool   `json:"credential_failed"`
	LastStartedAt       string `json:"last_started_at,omitempty"`
	LastObservedAt      string `json:"last_observed_at,omitempty"`
	LastCompletedAt     string `json:"last_completed_at,omitempty"`
	LastSuccessfulAt    string `json:"last_successful_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type domainBacklogJSON struct {
	Domain           string  `json:"domain"`
	Outstanding      int     `json:"outstanding"`
	InFlight         int     `json:"in_flight"`
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
			InFlight:         row.InFlight,
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

func registryCollectorsJSON(rows []RegistryCollectorSnapshot) []registryCollectorJSON {
	projected := make([]registryCollectorJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, registryCollectorJSON{
			CollectorKind:              row.CollectorKind,
			ConfiguredInstances:        row.ConfiguredInstances,
			ActiveScopes:               row.ActiveScopes,
			RecentCompletedGenerations: row.RecentCompletedGenerations,
			LastCompletedAt:            nullableRFC3339Value(row.LastCompletedAt),
			RetryableFailures:          row.RetryableFailures,
			TerminalFailures:           row.TerminalFailures,
			FailureClassCounts:         namedCountsJSON(row.FailureClassCounts),
		})
	}
	return projected
}

func awsCloudScansJSON(rows []AWSCloudScanStatus) []awsCloudScanJSON {
	projected := make([]awsCloudScanJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, awsCloudScanJSON{
			CollectorInstanceID: row.CollectorInstanceID,
			AccountID:           row.AccountID,
			Region:              row.Region,
			ServiceKind:         row.ServiceKind,
			Status:              row.Status,
			CommitStatus:        row.CommitStatus,
			FailureClass:        row.FailureClass,
			FailureMessage:      row.FailureMessage,
			APICallCount:        row.APICallCount,
			ThrottleCount:       row.ThrottleCount,
			WarningCount:        row.WarningCount,
			ResourceCount:       row.ResourceCount,
			RelationshipCount:   row.RelationshipCount,
			TagObservationCount: row.TagObservationCount,
			BudgetExhausted:     row.BudgetExhausted,
			CredentialFailed:    row.CredentialFailed,
			LastStartedAt:       nullableRFC3339Value(row.LastStartedAt),
			LastObservedAt:      nullableRFC3339Value(row.LastObservedAt),
			LastCompletedAt:     nullableRFC3339Value(row.LastCompletedAt),
			LastSuccessfulAt:    nullableRFC3339Value(row.LastSuccessfulAt),
			UpdatedAt:           nullableRFC3339Value(row.UpdatedAt),
		})
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
