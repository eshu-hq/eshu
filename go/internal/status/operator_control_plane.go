package status

import (
	"sort"
	"time"
)

// OperatorControlPlane is the unified operator read model for the control-plane
// epic: one projection that answers what is broken, stale, blocked, or safe to
// replay across the queue, reducer domains, collector families, and dead-letter
// state. It is a pure projection of an already-loaded Report and performs no
// I/O, so it adds no database or graph cost beyond the snapshot the caller has
// already loaded.
type OperatorControlPlane struct {
	// AsOf is the snapshot time inherited from the projected Report.
	AsOf time.Time
	// Health is the operator-facing health verdict and reasons.
	Health HealthSummary
	// Queue carries depth, claim-latency, stuck-work, and retry pressure.
	Queue OperatorQueueView
	// ReducerDomains lists reducer/projection domain backlogs, highest pressure
	// first, each retaining retry and dead-letter detail for drilldown.
	ReducerDomains []DomainBacklog
	// CollectorFamilies lists one promotion verdict per collector family with
	// the newest proof-artifact timestamps for correlation.
	CollectorFamilies []OperatorCollectorFamily
	// DeadLetters captures dead-letter pressure across the queue and the
	// collector-generation commit path, keyed by operator-facing class.
	DeadLetters OperatorDeadLetters
	// RetryPolicies is the active per-stage retry policy summary.
	RetryPolicies []RetryPolicySummary
}

// OperatorQueueView captures queue depth, claim-latency, stuck-work, and retry
// signals already computed on the Report's queue snapshot and coordinator.
type OperatorQueueView struct {
	// Total is every tracked work item regardless of status.
	Total int
	// Outstanding is pending+claimed+running+retrying work.
	Outstanding int
	// Pending is unclaimed work.
	Pending int
	// InFlight is claimed or running work.
	InFlight int
	// Retrying is work scheduled for another attempt.
	Retrying int
	// DeadLetter is terminal queue work awaiting operator action.
	DeadLetter int
	// OverdueClaims counts claims whose lease deadline has passed; a primary
	// claim-latency signal.
	OverdueClaims int
	// OldestOutstandingAge is the age of the oldest outstanding item; the
	// primary stuck-work signal.
	OldestOutstandingAge time.Duration
	// OldestPendingAge is the coordinator's oldest pending claim age, a
	// secondary claim-latency signal. Zero when no coordinator snapshot exists.
	OldestPendingAge time.Duration
	// BlockedConflicts counts distinct conflict-domain blockages holding work.
	BlockedConflicts int
}

// OperatorCollectorFamily is one collector family's promotion verdict, health,
// and newest proof-artifact timestamps, derived from the promotion-proof spine.
type OperatorCollectorFamily struct {
	// CollectorKind is the durable collector family identifier.
	CollectorKind string
	// DisplayName is the operator-facing family label.
	DisplayName string
	// PromotionState is the derived promotion verdict (implemented, gated, ...).
	PromotionState string
	// Health is the underlying runtime health, empty when no instance exists.
	Health string
	// ClaimState describes how the family executes (claim_driven, direct, ...).
	ClaimState string
	// ReducerReadback reports whether reducer-projected evidence is available.
	ReducerReadback string
	// LastObservedAt is the newest proof-artifact observation timestamp.
	LastObservedAt time.Time
	// TelemetryHandles lists stable metric and span names for diagnosis.
	TelemetryHandles []string
	// Blockers are safe, human-readable reasons the family is not implemented.
	Blockers []string
}

// OperatorDeadLetters captures dead-letter pressure for the read model. Queue
// dead letters are classed by reducer domain; collector-generation commit
// failures are summarized separately; the newest queue failure carries safe
// correlation IDs.
type OperatorDeadLetters struct {
	// QueueDeadLetter is the total dead-lettered queue work.
	QueueDeadLetter int
	// ByDomain lists reducer-domain dead-letter classes, highest count first.
	// Domains with zero dead letters are excluded.
	ByDomain []DomainDeadLetter
	// CollectorGeneration summarizes pre-queue collector commit dead letters.
	CollectorGeneration CollectorGenerationDeadLetterSnapshot
	// LatestFailureClass is the newest queue failure class, empty when none.
	LatestFailureClass string
	// LatestFailureDomain is the newest queue failure domain.
	LatestFailureDomain string
	// LatestFailureWorkItemID correlates the newest failure to queue rows.
	LatestFailureWorkItemID string
	// LatestFailureScopeID correlates the newest failure to a scope.
	LatestFailureScopeID string
	// LatestFailureGenerationID correlates the newest failure to a generation.
	LatestFailureGenerationID string
	// LatestFailureAt is the newest failure update time, zero when none.
	LatestFailureAt time.Time
}

// DomainDeadLetter is one reducer-domain dead-letter class and its age.
type DomainDeadLetter struct {
	// Domain is the reducer/projection domain name.
	Domain string
	// DeadLetter is the dead-letter count in this domain.
	DeadLetter int
	// OldestAge is the age of the oldest backlog item in this domain.
	OldestAge time.Duration
}

// ControlPlane projects an already-loaded Report into the unified operator read
// model. It performs no I/O. Collector families are enumerated from the
// promotion-proof spine so every known family yields at least one verdict.
func ControlPlane(report Report) OperatorControlPlane {
	return OperatorControlPlane{
		AsOf:              report.AsOf,
		Health:            report.Health,
		Queue:             controlPlaneQueue(report),
		ReducerDomains:    controlPlaneReducerDomains(report.DomainBacklogs),
		CollectorFamilies: controlPlaneCollectorFamilies(report),
		DeadLetters:       controlPlaneDeadLetters(report),
		// Clone so the read model never aliases the caller's slice; every other
		// projected slice is likewise freshly built.
		RetryPolicies: cloneRetryPolicies(report.RetryPolicies),
	}
}

// controlPlaneReducerDomains returns a copy of the domain backlogs sorted by
// operator pressure (highest outstanding, then oldest age, then name) so the
// read model is self-contained regardless of how the caller built the Report.
func controlPlaneReducerDomains(rows []DomainBacklog) []DomainBacklog {
	if len(rows) == 0 {
		return nil
	}
	sorted := make([]DomainBacklog, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Outstanding != sorted[j].Outstanding {
			return sorted[i].Outstanding > sorted[j].Outstanding
		}
		if sorted[i].OldestAge != sorted[j].OldestAge {
			return sorted[i].OldestAge > sorted[j].OldestAge
		}
		return sorted[i].Domain < sorted[j].Domain
	})
	return sorted
}

func controlPlaneQueue(report Report) OperatorQueueView {
	view := OperatorQueueView{
		Total:                report.Queue.Total,
		Outstanding:          report.Queue.Outstanding,
		Pending:              report.Queue.Pending,
		InFlight:             report.Queue.InFlight,
		Retrying:             report.Queue.Retrying,
		DeadLetter:           report.Queue.DeadLetter,
		OverdueClaims:        report.Queue.OverdueClaims,
		OldestOutstandingAge: report.Queue.OldestOutstandingAge,
		BlockedConflicts:     len(report.QueueBlockages),
	}
	if report.Coordinator != nil {
		view.OldestPendingAge = report.Coordinator.OldestPendingAge
	}
	return view
}

func controlPlaneCollectorFamilies(report Report) []OperatorCollectorFamily {
	// A nil catalog enumerates the full collector fleet so every known family
	// yields a verdict, including absent and unsupported families an operator
	// must still see in the read model.
	proofs := CollectorPromotionProofs(report, CollectorPromotionOptions{
		AsOf:       report.AsOf,
		StaleAfter: DefaultCollectorPromotionStaleAfter,
	})
	return rollupOperatorCollectorFamilies(proofs)
}

// rollupOperatorCollectorFamilies collapses instance-level promotion proofs to
// one verdict per collector family, keeping the newest observation and the worst
// (least-promoted) verdict so an operator sees the family as unhealthy when any
// instance is. The worst-verdict instance's runtime fields travel with it.
func rollupOperatorCollectorFamilies(proofs []CollectorPromotionProof) []OperatorCollectorFamily {
	byKind := map[string]*OperatorCollectorFamily{}
	order := make([]string, 0, len(proofs))
	for _, proof := range proofs {
		family, ok := byKind[proof.CollectorKind]
		if !ok {
			family = &OperatorCollectorFamily{
				CollectorKind:    proof.CollectorKind,
				DisplayName:      proof.DisplayName,
				PromotionState:   proof.PromotionState,
				Health:           proof.Health,
				ClaimState:       proof.ClaimState,
				ReducerReadback:  proof.ReducerReadback,
				LastObservedAt:   proof.LastObservedAt,
				TelemetryHandles: proof.TelemetryHandles,
				Blockers:         proof.Blockers,
			}
			byKind[proof.CollectorKind] = family
			order = append(order, proof.CollectorKind)
			continue
		}
		if proof.LastObservedAt.After(family.LastObservedAt) {
			family.LastObservedAt = proof.LastObservedAt
		}
		if collectorPromotionSeverity(proof.PromotionState) > collectorPromotionSeverity(family.PromotionState) {
			// Adopt every runtime field from the worse-verdict instance so the
			// rolled-up row never reports a failed/gated family while showing the
			// claim or readback state of a healthier sibling.
			family.PromotionState = proof.PromotionState
			family.Health = proof.Health
			family.Blockers = proof.Blockers
			family.ClaimState = proof.ClaimState
			family.ReducerReadback = proof.ReducerReadback
		}
	}

	sort.Strings(order)
	families := make([]OperatorCollectorFamily, 0, len(order))
	for _, kind := range order {
		families = append(families, *byKind[kind])
	}
	return families
}

// collectorPromotionSeverity ranks promotion verdicts so the family rolls up to
// its least-healthy instance. Higher is worse.
func collectorPromotionSeverity(state string) int {
	switch state {
	case CollectorPromotionImplemented:
		return 0
	case CollectorPromotionPartial:
		return 1
	case CollectorPromotionStale:
		return 2
	case CollectorPromotionGated:
		return 3
	case CollectorPromotionDisabled:
		return 4
	case CollectorPromotionPermissionHidden:
		return 5
	case CollectorPromotionUnsupported:
		return 6
	case CollectorPromotionFailed:
		return 7
	default:
		return 1
	}
}

func controlPlaneDeadLetters(report Report) OperatorDeadLetters {
	dead := OperatorDeadLetters{
		QueueDeadLetter:     report.Queue.DeadLetter,
		CollectorGeneration: report.CollectorGenerationDeadLetters,
	}

	for _, domain := range report.DomainBacklogs {
		if domain.DeadLetter <= 0 {
			continue
		}
		dead.ByDomain = append(dead.ByDomain, DomainDeadLetter{
			Domain:     domain.Domain,
			DeadLetter: domain.DeadLetter,
			OldestAge:  domain.OldestAge,
		})
	}
	sort.Slice(dead.ByDomain, func(i, j int) bool {
		if dead.ByDomain[i].DeadLetter != dead.ByDomain[j].DeadLetter {
			return dead.ByDomain[i].DeadLetter > dead.ByDomain[j].DeadLetter
		}
		return dead.ByDomain[i].Domain < dead.ByDomain[j].Domain
	})

	if failure := report.LatestQueueFailure; failure != nil {
		dead.LatestFailureClass = failure.FailureClass
		dead.LatestFailureDomain = failure.Domain
		dead.LatestFailureWorkItemID = failure.WorkItemID
		dead.LatestFailureScopeID = failure.ScopeID
		dead.LatestFailureGenerationID = failure.GenerationID
		dead.LatestFailureAt = failure.UpdatedAt
	}

	return dead
}
