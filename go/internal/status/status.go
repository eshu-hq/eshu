// Package status projects raw Go data-plane runtime counts into an
// operator-facing status report.
package status

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

const (
	healthHealthy     = "healthy"
	healthProgressing = "progressing"
	healthDegraded    = "degraded"
	healthStalled     = "stalled"
)

// NamedCount captures one status bucket and its count.
type NamedCount struct {
	Name  string
	Count int
}

// StageStatusCount captures one stage/status bucket from the work queue.
type StageStatusCount struct {
	Stage  string
	Status string
	Count  int
}

// ScopeActivitySnapshot captures the incremental-refresh operator counters
// that distinguish active scopes from scopes with a newer pending generation.
type ScopeActivitySnapshot struct {
	Active    int
	Changed   int
	Unchanged int
}

// QueueSnapshot captures aggregate queue pressure and progress signals.
type QueueSnapshot struct {
	Total                int
	Outstanding          int
	Pending              int
	InFlight             int
	Retrying             int
	Succeeded            int
	Failed               int
	DeadLetter           int
	OldestOutstandingAge time.Duration
	OverdueClaims        int
}

// QueueFailureSnapshot captures the newest queued work failure metadata shown
// on operator status surfaces. Values are rendered only in status payloads and
// must not be promoted to metric labels.
type QueueFailureSnapshot struct {
	Stage          string
	Domain         string
	Status         string
	WorkItemID     string
	ScopeID        string
	GenerationID   string
	FailureClass   string
	FailureMessage string
	FailureDetails string
	UpdatedAt      time.Time
}

// DomainBacklog captures backlog depth for one reducer or projection domain.
type DomainBacklog struct {
	Domain      string
	Outstanding int
	Retrying    int
	Failed      int
	DeadLetter  int
	OldestAge   time.Duration
}

// RawSnapshot is the read-only substrate snapshot gathered from Postgres.
type RawSnapshot struct {
	AsOf                  time.Time
	ScopeCounts           []NamedCount
	GenerationCounts      []NamedCount
	ScopeActivity         ScopeActivitySnapshot
	GenerationHistory     GenerationHistorySnapshot
	GenerationTransitions []GenerationTransitionSnapshot
	StageCounts           []StageStatusCount
	DomainBacklogs        []DomainBacklog
	QueueBlockages        []QueueBlockage
	RetryPolicies         []RetryPolicySummary
	Queue                 QueueSnapshot
	LatestQueueFailure    *QueueFailureSnapshot
	Coordinator           *CoordinatorSnapshot
}

// Reader loads the raw status snapshot from an underlying storage backend.
type Reader interface {
	ReadStatusSnapshot(context.Context, time.Time) (RawSnapshot, error)
}

// Options controls operator-health projection behavior.
type Options struct {
	StallAfter  time.Duration
	DomainLimit int
}

// HealthSummary captures the operator-facing health verdict and reasons.
type HealthSummary struct {
	State   string   `json:"state"`
	Reasons []string `json:"reasons"`
}

// StageSummary collapses queue counts into one row per stage.
type StageSummary struct {
	Stage      string `json:"stage"`
	Pending    int    `json:"pending"`
	Claimed    int    `json:"claimed"`
	Running    int    `json:"running"`
	Retrying   int    `json:"retrying"`
	Succeeded  int    `json:"succeeded"`
	Failed     int    `json:"failed"`
	DeadLetter int    `json:"dead_letter"`
}

// Report is the operator-facing summary rendered by CLI and future admin APIs.
type Report struct {
	AsOf                  time.Time
	Health                HealthSummary
	FlowSummaries         []FlowSummary
	Queue                 QueueSnapshot
	RetryPolicies         []RetryPolicySummary
	ScopeActivity         ScopeActivitySnapshot
	GenerationHistory     GenerationHistorySnapshot
	GenerationTransitions []GenerationTransitionSnapshot
	ScopeTotals           map[string]int
	GenerationTotals      map[string]int
	StageSummaries        []StageSummary
	DomainBacklogs        []DomainBacklog
	QueueBlockages        []QueueBlockage
	LatestQueueFailure    *QueueFailureSnapshot
	Coordinator           *CoordinatorSnapshot
}

// DefaultOptions returns the baseline operator heuristics for this first live
// status surface.
func DefaultOptions() Options {
	return Options{
		StallAfter:  10 * time.Minute,
		DomainLimit: 5,
	}
}

// LoadReport reads one snapshot through the shared reader contract and
// projects it into an operator-facing report.
func LoadReport(ctx context.Context, reader Reader, asOf time.Time, opts Options) (Report, error) {
	if reader == nil {
		return Report{}, fmt.Errorf("status reader is required")
	}

	raw, err := reader.ReadStatusSnapshot(ctx, asOf.UTC())
	if err != nil {
		return Report{}, fmt.Errorf("read status snapshot: %w", err)
	}

	return BuildReport(raw, opts), nil
}

// BuildReport projects one raw substrate snapshot into an operator-facing
// report.
func BuildReport(raw RawSnapshot, opts Options) Report {
	if opts.StallAfter <= 0 {
		opts.StallAfter = DefaultOptions().StallAfter
	}
	if opts.DomainLimit <= 0 {
		opts.DomainLimit = DefaultOptions().DomainLimit
	}

	scopeTotals := toCountMap(raw.ScopeCounts)
	generationTotals := toCountMap(raw.GenerationCounts)
	scopeActivity := raw.ScopeActivity
	if scopeActivity == (ScopeActivitySnapshot{}) {
		scopeActivity = deriveScopeActivity(scopeTotals, generationTotals)
	} else if scopeActivity.Unchanged == 0 {
		scopeActivity.Unchanged = scopeUnchangedCount(scopeActivity.Active, scopeActivity.Changed)
	}
	generationHistory := raw.GenerationHistory
	if generationHistoryIsZero(generationHistory) {
		generationHistory = deriveGenerationHistory(generationTotals)
	}
	stageSummaries := summarizeStages(raw.StageCounts)
	domainBacklogs := topDomainBacklogs(raw.DomainBacklogs, opts.DomainLimit)
	flowSummaries := buildFlowSummaries(scopeTotals, generationTotals, stageSummaries, raw.Queue, domainBacklogs)

	return Report{
		AsOf:                  raw.AsOf,
		Health:                evaluateHealth(raw.Queue, generationTotals, domainBacklogs, opts),
		FlowSummaries:         flowSummaries,
		Queue:                 raw.Queue,
		RetryPolicies:         cloneRetryPolicies(raw.RetryPolicies),
		ScopeActivity:         scopeActivity,
		GenerationHistory:     generationHistory,
		GenerationTransitions: cloneGenerationTransitions(raw.GenerationTransitions),
		ScopeTotals:           scopeTotals,
		GenerationTotals:      generationTotals,
		StageSummaries:        stageSummaries,
		DomainBacklogs:        domainBacklogs,
		QueueBlockages:        cloneQueueBlockages(raw.QueueBlockages),
		LatestQueueFailure:    cloneQueueFailure(raw.LatestQueueFailure),
		Coordinator:           cloneCoordinatorSnapshot(raw.Coordinator),
	}
}

func deriveScopeActivity(scopeTotals map[string]int, generationTotals map[string]int) ScopeActivitySnapshot {
	activeScopes := scopeTotals["active"]
	pendingGenerations := generationTotals["pending"]
	if pendingGenerations > activeScopes {
		pendingGenerations = activeScopes
	}

	return ScopeActivitySnapshot{
		Active:    activeScopes,
		Changed:   pendingGenerations,
		Unchanged: scopeUnchangedCount(activeScopes, pendingGenerations),
	}
}

// RenderText returns a compact admin-panel-style text summary.
func RenderText(report Report) string {
	lines := []string{
		fmt.Sprintf("Version: %s", buildinfo.AppVersion()),
		fmt.Sprintf("Health: %s", report.Health.State),
		fmt.Sprintf(
			"Queue: outstanding=%d in_flight=%d retrying=%d dead_letter=%d failed=%d oldest=%s overdue_claims=%d",
			report.Queue.Outstanding,
			report.Queue.InFlight,
			report.Queue.Retrying,
			report.Queue.DeadLetter,
			report.Queue.Failed,
			report.Queue.OldestOutstandingAge,
			report.Queue.OverdueClaims,
		),
		fmt.Sprintf("Retry policies: %s", retryPoliciesText(report.RetryPolicies)),
		fmt.Sprintf(
			"Scope activity: %s",
			scopeActivityText(report.ScopeActivity),
		),
		fmt.Sprintf("Scope statuses: %s", formatNamedTotals(report.ScopeTotals)),
		fmt.Sprintf("Generation history: %s", generationHistoryText(report.GenerationHistory)),
		fmt.Sprintf("Generation transitions: %s", generationTransitionsText(report.GenerationTransitions)),
	}

	if len(report.Health.Reasons) > 0 {
		lines = append(lines, fmt.Sprintf("Reasons: %s", strings.Join(report.Health.Reasons, "; ")))
	}
	if latestFailure := queueFailureText(report.LatestQueueFailure); latestFailure != "" {
		lines = append(lines, fmt.Sprintf("Latest queue failure: %s", latestFailure))
	}
	lines = append(lines, renderQueueBlockageLines(report.QueueBlockages)...)
	lines = append(lines, renderCoordinatorLines(report.Coordinator)...)
	lines = append(lines, renderFlowLines(report.FlowSummaries)...)
	if len(report.StageSummaries) > 0 {
		lines = append(lines, "Stages:")
		for _, row := range report.StageSummaries {
			lines = append(
				lines,
				fmt.Sprintf(
					"  %s pending=%d claimed=%d running=%d retrying=%d succeeded=%d dead_letter=%d failed=%d",
					row.Stage,
					row.Pending,
					row.Claimed,
					row.Running,
					row.Retrying,
					row.Succeeded,
					row.DeadLetter,
					row.Failed,
				),
			)
		}
	}
	if len(report.DomainBacklogs) > 0 {
		lines = append(lines, "Domains:")
		for _, row := range report.DomainBacklogs {
			lines = append(
				lines,
				fmt.Sprintf(
					"  %s outstanding=%d retrying=%d dead_letter=%d failed=%d oldest=%s",
					row.Domain,
					row.Outstanding,
					row.Retrying,
					row.DeadLetter,
					row.Failed,
					row.OldestAge,
				),
			)
		}
	}

	return strings.Join(lines, "\n")
}

func summarizeStages(rows []StageStatusCount) []StageSummary {
	byStage := make(map[string]*StageSummary, len(rows))
	for _, row := range rows {
		stageName := strings.TrimSpace(row.Stage)
		if stageName == "" {
			continue
		}
		stageSummary, ok := byStage[stageName]
		if !ok {
			stageSummary = &StageSummary{Stage: stageName}
			byStage[stageName] = stageSummary
		}
		switch strings.TrimSpace(row.Status) {
		case "pending":
			stageSummary.Pending += row.Count
		case "claimed":
			stageSummary.Claimed += row.Count
		case "running":
			stageSummary.Running += row.Count
		case "retrying":
			stageSummary.Retrying += row.Count
		case "succeeded":
			stageSummary.Succeeded += row.Count
		case "failed":
			stageSummary.Failed += row.Count
		case "dead_letter":
			stageSummary.DeadLetter += row.Count
		}
	}

	stageNames := make([]string, 0, len(byStage))
	for stageName := range byStage {
		stageNames = append(stageNames, stageName)
	}
	sort.Strings(stageNames)

	summaries := make([]StageSummary, 0, len(stageNames))
	for _, stageName := range stageNames {
		summaries = append(summaries, *byStage[stageName])
	}

	return summaries
}

func topDomainBacklogs(rows []DomainBacklog, limit int) []DomainBacklog {
	filtered := make([]DomainBacklog, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Domain) == "" {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Outstanding != filtered[j].Outstanding {
			return filtered[i].Outstanding > filtered[j].Outstanding
		}
		if filtered[i].OldestAge != filtered[j].OldestAge {
			return filtered[i].OldestAge > filtered[j].OldestAge
		}
		return filtered[i].Domain < filtered[j].Domain
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered
}

func toCountMap(rows []NamedCount) map[string]int {
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		counts[name] += row.Count
	}

	return counts
}

func cloneCounts(values map[string]int) map[string]int {
	if len(values) == 0 {
		return map[string]int{}
	}
	cloned := make(map[string]int, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func formatNamedTotals(values map[string]int) string {
	if len(values) == 0 {
		return "none"
	}

	keys := make([]string, 0, len(values))
	for key, value := range values {
		if value <= 0 {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return countOrder(keys[i]) < countOrder(keys[j]) ||
			(countOrder(keys[i]) == countOrder(keys[j]) && keys[i] < keys[j])
	})

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, " ")
}

func countOrder(name string) int {
	switch name {
	case "active":
		return 0
	case "pending":
		return 1
	case "completed":
		return 2
	case "succeeded":
		return 3
	case "failed":
		return 4
	default:
		return 100
	}
}
