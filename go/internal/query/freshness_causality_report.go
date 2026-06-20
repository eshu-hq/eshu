package query

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// Freshness causality observability classes. A runtime cause is visible from
// the cluster status snapshot; a per-answer cause only surfaces on an
// individual answer's truth envelope and cannot be observed cluster-wide.
const (
	freshnessObservabilityRuntime   = "runtime"
	freshnessObservabilityPerAnswer = "per_answer"
)

// Overall freshness causality states, aligned with TruthFreshness.
const (
	freshnessCausalityFresh    = "fresh"
	freshnessCausalityBuilding = "building"
	freshnessCausalityStale    = "stale"
)

// FreshnessCausality is the operator read model for stale-answer and graph
// retraction causality. It enumerates every closed FreshnessCause, marks which
// are currently observed in the runtime, and summarizes the generation lifecycle
// and pending projection work that drive catch-up. It is a pure projection of an
// already-loaded status Report and performs no I/O.
type FreshnessCausality struct {
	// State is the overall freshness verdict (fresh|building|stale).
	State string
	// Causes enumerates all seven closed freshness causes with observation.
	Causes []FreshnessCauseStatus
	// Generations summarizes the active/pending/retired generation lifecycle.
	Generations FreshnessGenerations
	// PendingProjection summarizes outstanding and dead-lettered projection work.
	PendingProjection FreshnessPendingProjection
	// RecentTransitions are recent generation lifecycle rows (activations and
	// supersessions/retractions) for causality drilldown.
	RecentTransitions []FreshnessTransition
}

// FreshnessCauseStatus is one closed cause, whether it is currently observed,
// how it can be observed, and its bounded drilldown.
type FreshnessCauseStatus struct {
	Cause         FreshnessCause
	Observed      bool
	Observability string
	Detail        string
	NextCheck     FreshnessNextCheck
}

// FreshnessGenerations summarizes the generation lifecycle. Superseded counts
// retired generations whose evidence has been or will be retracted.
type FreshnessGenerations struct {
	Active     int
	Pending    int
	Completed  int
	Superseded int
	Failed     int
}

// FreshnessPendingProjection summarizes projection work still owed before the
// graph catches up to the active generation.
type FreshnessPendingProjection struct {
	Outstanding int
	DeadLetter  int
	Domains     int
}

// FreshnessTransition is one recent generation lifecycle row.
type FreshnessTransition struct {
	ScopeID       string
	GenerationID  string
	Status        string
	TriggerKind   string
	FreshnessHint string
	ObservedAt    time.Time
	SupersededAt  time.Time
}

// freshnessCausalityFromReport projects a status Report into the freshness
// causality read model without any I/O.
func freshnessCausalityFromReport(report status.Report) FreshnessCausality {
	return freshnessCausalityFromRawAndReport(status.RawSnapshot{
		DomainBacklogs: report.DomainBacklogs,
	}, report)
}

// freshnessCausalityFromRawAndReport projects freshness causality from the
// uncapped raw snapshot plus the normalized status report. Pending projection
// totals must use raw domain backlog rows because Report.DomainBacklogs is a
// top-domain preview capped for status rendering.
func freshnessCausalityFromRawAndReport(raw status.RawSnapshot, report status.Report) FreshnessCausality {
	signals := deriveFreshnessSignals(raw, report)

	fc := FreshnessCausality{
		Causes:      buildFreshnessCauseStatuses(signals),
		Generations: freshnessGenerations(report.GenerationHistory),
		PendingProjection: FreshnessPendingProjection{
			Outstanding: signals.outstanding,
			DeadLetter:  signals.deadLetter,
			Domains:     signals.backlogDomains,
		},
		RecentTransitions: freshnessTransitions(report.GenerationTransitions),
	}
	fc.State = freshnessState(signals)
	return fc
}

// freshnessSignals are the runtime-observable inputs derived once from the
// report and reused for every cause and the overall state.
type freshnessSignals struct {
	pendingGenerations bool
	reducerBacklog     bool
	deadLetteredDomain bool
	missingCompletion  bool
	outstanding        int
	deadLetter         int
	backlogDomains     int
}

func deriveFreshnessSignals(raw status.RawSnapshot, report status.Report) freshnessSignals {
	var s freshnessSignals
	s.pendingGenerations = report.GenerationHistory.Pending > 0
	domainBacklogs := raw.DomainBacklogs
	if len(domainBacklogs) == 0 {
		domainBacklogs = report.DomainBacklogs
	}
	for _, d := range domainBacklogs {
		s.outstanding += d.Outstanding
		s.deadLetter += d.DeadLetter
		if d.Outstanding > 0 || d.DeadLetter > 0 {
			s.backlogDomains++
		}
		if d.Outstanding > 0 {
			s.reducerBacklog = true
		}
		if d.DeadLetter > 0 {
			s.deadLetteredDomain = true
		}
	}
	if report.Queue.DeadLetter > 0 || report.CollectorGenerationDeadLetters.DeadLetter > 0 {
		s.deadLetteredDomain = true
	}
	if report.CollectorGenerationDeadLetters.DeadLetter > 0 {
		s.missingCompletion = true
	}
	if c := report.Coordinator; c != nil && c.RecentFailures != nil &&
		(c.RecentFailures.BlockedCompleteness > 0 || c.RecentFailures.FailedRuns > 0) {
		s.missingCompletion = true
	}
	return s
}

// freshnessState ranks the runtime signals: any stuck signal is stale, any
// catch-up signal is building, otherwise fresh.
func freshnessState(s freshnessSignals) string {
	if s.deadLetteredDomain || s.missingCompletion {
		return freshnessCausalityStale
	}
	if s.pendingGenerations || s.reducerBacklog {
		return freshnessCausalityBuilding
	}
	return freshnessCausalityFresh
}

func buildFreshnessCauseStatuses(s freshnessSignals) []FreshnessCauseStatus {
	runtimeObserved := map[FreshnessCause]bool{
		FreshnessCausePendingRepoGeneration:      s.pendingGenerations,
		FreshnessCauseReducerBacklog:             s.reducerBacklog,
		FreshnessCauseDeadLetteredDomain:         s.deadLetteredDomain,
		FreshnessCauseMissingCollectorCompletion: s.missingCompletion,
	}
	perAnswer := map[FreshnessCause]bool{
		FreshnessCauseContentCoverageUnavailable: true,
		FreshnessCauseUnsupportedProfile:         true,
		FreshnessCauseRetentionExpired:           true,
	}

	statuses := make([]FreshnessCauseStatus, 0, len(orderedFreshnessCauses))
	for _, cause := range orderedFreshnessCauses {
		nextCheck, _ := FreshnessCauseNextCheck(cause)
		observability := freshnessObservabilityRuntime
		if perAnswer[cause] {
			observability = freshnessObservabilityPerAnswer
		}
		statuses = append(statuses, FreshnessCauseStatus{
			Cause:         cause,
			Observed:      runtimeObserved[cause],
			Observability: observability,
			Detail:        nextCheck.Reason,
			NextCheck:     nextCheck,
		})
	}
	return statuses
}

// orderedFreshnessCauses lists the closed causes in a stable, operator-facing
// order: runtime catch-up first, then stuck, then per-answer classes.
var orderedFreshnessCauses = []FreshnessCause{
	FreshnessCausePendingRepoGeneration,
	FreshnessCauseReducerBacklog,
	FreshnessCauseDeadLetteredDomain,
	FreshnessCauseMissingCollectorCompletion,
	FreshnessCauseContentCoverageUnavailable,
	FreshnessCauseUnsupportedProfile,
	FreshnessCauseRetentionExpired,
}

func freshnessGenerations(h status.GenerationHistorySnapshot) FreshnessGenerations {
	return FreshnessGenerations{
		Active:     h.Active,
		Pending:    h.Pending,
		Completed:  h.Completed,
		Superseded: h.Superseded,
		Failed:     h.Failed,
	}
}

func freshnessTransitions(rows []status.GenerationTransitionSnapshot) []FreshnessTransition {
	out := make([]FreshnessTransition, 0, len(rows))
	for _, row := range rows {
		out = append(out, FreshnessTransition{
			ScopeID:       row.ScopeID,
			GenerationID:  row.GenerationID,
			Status:        row.Status,
			TriggerKind:   row.TriggerKind,
			FreshnessHint: row.FreshnessHint,
			ObservedAt:    row.ObservedAt,
			SupersededAt:  row.SupersededAt,
		})
	}
	return out
}
