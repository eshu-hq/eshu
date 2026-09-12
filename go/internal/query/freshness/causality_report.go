// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

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

// Causality is the operator read model for stale-answer and graph
// retraction causality. It enumerates every closed Cause, marks which
// are currently observed in the runtime, and summarizes the generation lifecycle
// and pending projection work that drive catch-up. It is a pure projection of an
// already-loaded status Report and performs no I/O.
type Causality struct {
	// State is the overall freshness verdict (fresh|building|stale).
	State string
	// Causes enumerates all seven closed freshness causes with observation.
	Causes []CauseStatus
	// Generations summarizes the active/pending/retired generation lifecycle.
	Generations Generations
	// PendingProjection summarizes outstanding and dead-lettered projection work.
	PendingProjection PendingProjection
	// RecentTransitions are recent generation lifecycle rows (activations and
	// supersessions/retractions) for causality drilldown.
	RecentTransitions []Transition
}

// CauseStatus is one closed cause, whether it is currently observed,
// how it can be observed, and its bounded drilldown.
type CauseStatus struct {
	Cause         Cause
	Observed      bool
	Observability string
	Detail        string
	NextCheck     NextCheck
}

// Generations summarizes the generation lifecycle. Superseded counts
// retired generations whose evidence has been or will be retracted.
type Generations struct {
	Active     int
	Pending    int
	Completed  int
	Superseded int
	Failed     int
}

// PendingProjection summarizes projection work still owed before the
// graph catches up to the active generation.
type PendingProjection struct {
	Outstanding int
	DeadLetter  int
	Domains     int
}

// Transition is one recent generation lifecycle row.
type Transition struct {
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
func freshnessCausalityFromReport(report status.Report) Causality {
	return CausalityFromRawAndReport(status.RawSnapshot{
		DomainBacklogs: report.DomainBacklogs,
	}, report)
}

// CausalityFromRawAndReport projects freshness causality from the
// uncapped raw snapshot plus the normalized status report. Pending projection
// totals must use raw domain backlog rows because Report.DomainBacklogs is a
// top-domain preview capped for status rendering. Exported (#6642) because
// root package query's staying status_freshness_causality.go
// (StatusHandler.getFreshnessCausality) calls it through the
// freshnessCausalityFromRawAndReport forwarder in freshness_alias.go.
func CausalityFromRawAndReport(raw status.RawSnapshot, report status.Report) Causality {
	signals := deriveFreshnessSignals(raw, report)

	fc := Causality{
		Causes:      buildFreshnessCauseStatuses(signals),
		Generations: freshnessGenerations(report.GenerationHistory),
		PendingProjection: PendingProjection{
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

func buildFreshnessCauseStatuses(s freshnessSignals) []CauseStatus {
	runtimeObserved := map[Cause]bool{
		CausePendingRepoGeneration:      s.pendingGenerations,
		CauseReducerBacklog:             s.reducerBacklog,
		CauseDeadLetteredDomain:         s.deadLetteredDomain,
		CauseMissingCollectorCompletion: s.missingCompletion,
	}
	perAnswer := map[Cause]bool{
		CauseContentCoverageUnavailable: true,
		CauseUnsupportedProfile:         true,
		CauseRetentionExpired:           true,
	}

	statuses := make([]CauseStatus, 0, len(orderedFreshnessCauses))
	for _, cause := range orderedFreshnessCauses {
		nextCheck, _ := CauseNextCheck(cause)
		observability := freshnessObservabilityRuntime
		if perAnswer[cause] {
			observability = freshnessObservabilityPerAnswer
		}
		statuses = append(statuses, CauseStatus{
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
var orderedFreshnessCauses = []Cause{
	CausePendingRepoGeneration,
	CauseReducerBacklog,
	CauseDeadLetteredDomain,
	CauseMissingCollectorCompletion,
	CauseContentCoverageUnavailable,
	CauseUnsupportedProfile,
	CauseRetentionExpired,
}

func freshnessGenerations(h status.GenerationHistorySnapshot) Generations {
	return Generations{
		Active:     h.Active,
		Pending:    h.Pending,
		Completed:  h.Completed,
		Superseded: h.Superseded,
		Failed:     h.Failed,
	}
}

func freshnessTransitions(rows []status.GenerationTransitionSnapshot) []Transition {
	out := make([]Transition, 0, len(rows))
	for _, row := range rows {
		out = append(out, Transition{
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
