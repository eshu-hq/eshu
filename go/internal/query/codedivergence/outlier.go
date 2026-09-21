// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// KindConventionOutlier names findings where members of a same-role cohort
// do not call a callee the cohort majority calls: N of M handlers call the
// guard, the rest are the outliers. The Fingerprint carries the cohort
// address plus the majority callee (see OutlierFingerprint), so investigate
// re-derives the same cohort and callee deterministically.
const KindConventionOutlier Kind = "parallel_implementation.convention_outlier"

// CohortSource names which cohort definition produced an outlier cohort, in
// trust order: implementers of one interface (explicit implements keyword,
// resolution_method=declared, strongest), handlers registered on one
// framework router (HANDLES_ROUTE edges from parser framework-route
// evidence), then same-role siblings in one package (the broadest fallback).
// Every finding names its source, so a maintainer knows what "same role"
// meant for that cohort.
type CohortSource string

const (
	// CohortInterface groups the Functions contained in the Classes, Structs,
	// or Enums that implement one Interface or Protocol entity.
	CohortInterface CohortSource = "interface"
	// CohortRouter groups the Functions with HANDLES_ROUTE edges to endpoints
	// under one mount (the first endpoint-path segment: the prefix one
	// framework router serves).
	CohortRouter CohortSource = "router"
	// CohortPackage groups the Functions contained in Files in one package
	// directory.
	CohortPackage CohortSource = "package"
)

// Convention-outlier reason codes. The majority and missing-call reasons are
// value-bearing and decompose members × tokens without remainder; the
// cohort, inferred-edge, mediation, and truncation reasons are zero-weight
// signals.
const (
	ReasonOutlierMajority  = "outlier_majority_calls"
	ReasonOutlierMissing   = "outlier_missing_call"
	ReasonOutlierCohort    = "outlier_cohort_source"
	ReasonOutlierInferred  = "outlier_inferred_edges"
	ReasonOutlierMediated  = "outlier_wrapper_mediated"
	ReasonOutlierTruncated = "outlier_cohort_truncated"
)

// Convention-outlier suppression rules, reported per rule in every response.
const (
	RuleBelowMinCohort          = "below_min_cohort"
	RuleOutlierUnqualified      = "outlier_unqualified"
	RuleCohortTruncated         = "cohort_truncated"
	RuleOutlierGraphUnavailable = "outlier_graph_unavailable"
	// RuleOutlierGraphTimeout counts a cohort sweep the bounded graph-read
	// budget cut short. Only the divergence report uses it: the rollup
	// degrades one slow family to a counted timeout instead of failing the
	// other four kinds with it. The findings page keeps the loud deadline
	// so a slow sweep stays visible as a defect, not a quiet zero.
	RuleOutlierGraphTimeout = "outlier_graph_timeout"
)

// OutlierParams tunes convention-outlier selection. The defaults are
// starters measured against the theory fixture and Eshu dogfood; the read
// surface exposes them, never silently applies them.
type OutlierParams struct {
	// MinCohortSize is the smallest cohort that can produce a finding.
	MinCohortSize int
	// MinShare is the minimum fraction of a cohort that must call a callee
	// for its non-callers to count as outliers.
	MinShare float64
	// MaxCohortSize caps the analyzed members per cohort. Cohorts above the
	// cap analyze their first members in entity-id order and report the
	// cutoff, never silently sample.
	MaxCohortSize int
}

// DefaultOutlierParams returns the shipped selection floors.
func DefaultOutlierParams() OutlierParams {
	return OutlierParams{
		MinCohortSize: 3,
		MinShare:      0.6,
		MaxCohortSize: 50,
	}
}

// OutlierCallerEdge is one outgoing CALLS edge from a cohort member: the
// callee identity plus the resolving edge's provenance (the confidence and
// inferred inputs, weakest-edge wins).
type OutlierCallerEdge struct {
	// CalleeID is the called entity's id.
	CalleeID string
	// CalleeName is the called entity's display name.
	CalleeName string
	// EdgeMethod is the ADR #2222 resolution method that produced the edge.
	EdgeMethod string
	// EdgeConfidence is the edge's derived confidence.
	EdgeConfidence float64
}

// OutlierMediation records an outlier that reaches the majority callee
// through one wrapper hop: the outlier calls MediatorID, which calls the
// callee. Deeper delegation chains are not flagged here; they surface as
// plain outliers, still factually correct. The finding still assembles (the
// direct-call convention is genuinely broken) but carries the mediation as
// an ambiguity signal pointing at the wrapper_bypass surface for the
// canonical-wrapper verdict.
type OutlierMediation struct {
	// OutlierID is the mediated cohort member.
	OutlierID string
	// MediatorID is the intermediate function the outlier calls.
	MediatorID string
	// MediatorName is the intermediate function's display name.
	MediatorName string
}

// OutlierVerdict is the selection outcome for one majority callee: the
// callers and outliers (partitioning the analyzed members), the measured
// share, the weakest majority-edge confidence, and whether any majority
// edge is inferred (surfaced, never silent).
type OutlierVerdict struct {
	// CalleeID is the majority callee's entity id.
	CalleeID string
	// CalleeName is the majority callee's display name.
	CalleeName string
	// CallerIDs are the analyzed members calling the callee, in id order.
	CallerIDs []string
	// OutlierIDs are the analyzed members not calling the callee, in id order.
	OutlierIDs []string
	// Share is the caller fraction over the analyzed members.
	Share float64
	// Confidence is the weakest majority CALLS-edge confidence.
	Confidence float64
	// Inferred reports a majority resting on at least one inferred edge.
	Inferred bool
	// Mediated carries the (callee, outlier) wrapper mediations, in outlier order.
	Mediated []OutlierMediation
}

// OutlierDetail is the finding's cohort evidence: which definition produced
// the cohort, the majority callee, the measured share, and the outliers.
// It rides Finding.Outlier for the convention_outlier kind only.
type OutlierDetail struct {
	// Source names the cohort definition that produced the cohort.
	Source CohortSource `json:"source"`
	// Key is the cohort address: interface entity id, router mount, or package directory.
	Key string `json:"key"`
	// Label is the human cohort name: interface name, mount routes, or package directory.
	Label string `json:"label"`
	// CohortSize is the analyzed finding member count (post-suppression survivors).
	CohortSize int `json:"cohort_size"`
	// Truncated reports a cohort cut at the member cap for bounded analysis.
	Truncated bool `json:"truncated"`
	// TotalCohort is the pre-truncation member count.
	TotalCohort int `json:"total_cohort"`
	// CalleeID is the majority callee's entity id.
	CalleeID string `json:"majority_callee_id"`
	// CalleeName is the majority callee's display name.
	CalleeName string `json:"majority_callee_name"`
	// Share is the caller fraction over the finding members.
	Share float64 `json:"share"`
	// CallerIDs are the finding members calling the callee, in id order.
	CallerIDs []string `json:"caller_ids"`
	// OutlierIDs are the finding members not calling the callee, in id order.
	OutlierIDs []string `json:"outlier_ids"`
	// Confidence is the weakest surviving majority CALLS-edge confidence.
	Confidence float64 `json:"confidence"`
	// Inferred reports a surviving majority resting on at least one inferred edge.
	Inferred bool `json:"inferred"`
	// Mediated carries the surviving wrapper mediations.
	Mediated []OutlierMediation `json:"mediated_outliers,omitempty"`
}

// SelectOutliers partitions one cohort per majority callee. For each callee
// invoked by at least MinShare of the analyzed members, the non-callers
// become a verdict carrying the cohort, the majority callee, the share, and
// the outliers. Unanimous callees (no outliers) and below-share callees
// produce nothing. Confidence is the weakest majority CALLS edge; a majority
// resting on inferred edges is labelled accordingly. Mediations attach per
// (callee, outlier) from the caller's two-hop evidence.
func SelectOutliers(
	cohort OutlierCohort,
	edges map[string][]OutlierCallerEdge,
	mediated map[string]map[string]OutlierMediation,
	params OutlierParams,
) []OutlierVerdict {
	members := append([]string(nil), cohort.Members...)
	sort.Strings(members)
	if len(members) > params.MaxCohortSize && params.MaxCohortSize > 0 {
		members = members[:params.MaxCohortSize]
	}
	if len(members) < params.MinCohortSize {
		return nil
	}
	type calleeTally struct {
		name     string
		callers  map[string]struct{}
		weakest  float64
		inferred bool
		hasEdge  bool
	}
	tallies := map[string]*calleeTally{}
	for _, member := range members {
		seen := map[string]struct{}{}
		for _, edge := range edges[member] {
			if edge.CalleeID == "" {
				continue
			}
			tally, ok := tallies[edge.CalleeID]
			if !ok {
				tally = &calleeTally{callers: map[string]struct{}{}}
				tallies[edge.CalleeID] = tally
			}
			if edge.CalleeName != "" && tally.name == "" {
				tally.name = edge.CalleeName
			}
			// One caller counts once per callee; the weakest duplicate
			// edge still sets the confidence floor.
			if !tally.hasEdge || edge.EdgeConfidence < tally.weakest {
				tally.weakest = edge.EdgeConfidence
				tally.hasEdge = true
			}
			if codeprovenance.IsInferred(codeprovenance.Method(edge.EdgeMethod)) {
				tally.inferred = true
			}
			if _, dup := seen[edge.CalleeID]; dup {
				continue
			}
			seen[edge.CalleeID] = struct{}{}
			tally.callers[member] = struct{}{}
		}
	}
	calleeIDs := make([]string, 0, len(tallies))
	for id := range tallies {
		calleeIDs = append(calleeIDs, id)
	}
	sort.Strings(calleeIDs)
	verdicts := []OutlierVerdict{}
	for _, calleeID := range calleeIDs {
		tally := tallies[calleeID]
		share := float64(len(tally.callers)) / float64(len(members))
		if share < params.MinShare {
			continue
		}
		if len(tally.callers) == len(members) {
			continue
		}
		callers := make([]string, 0, len(tally.callers))
		outliers := make([]string, 0, len(members)-len(tally.callers))
		for _, member := range members {
			if _, ok := tally.callers[member]; ok {
				callers = append(callers, member)
			} else {
				outliers = append(outliers, member)
			}
		}
		verdict := OutlierVerdict{
			CalleeID:   calleeID,
			CalleeName: tally.name,
			CallerIDs:  callers,
			OutlierIDs: outliers,
			Share:      share,
			Confidence: tally.weakest,
			Inferred:   tally.inferred,
		}
		for _, outlier := range outliers {
			if mediation, ok := mediated[calleeID][outlier]; ok {
				verdict.Mediated = append(verdict.Mediated, mediation)
			}
		}
		sort.Slice(verdict.Mediated, func(i, j int) bool {
			return verdict.Mediated[i].OutlierID < verdict.Mediated[j].OutlierID
		})
		verdicts = append(verdicts, verdict)
	}
	return verdicts
}

// AssembleOutlierFinding builds the finding for one majority-callee verdict.
// Content members resolve by entity id; the shared suppression catalogue
// (floor, generated, vendored, test files, trivial accessors) applies first,
// then the verdict re-checks over survivors so an emitted finding's members,
// share, and confidence always agree: at least two survivors, at least one
// surviving caller and one surviving outlier, and the surviving share still
// at the floor. Anything else drops under outlier_unqualified, counted, never
// silent. Outliers lead the member order (they are the finding's subject),
// then callers, each in entity-id order.
func AssembleOutlierFinding(
	repoID string,
	cohort OutlierCohort,
	members []Member,
	edges map[string][]OutlierCallerEdge,
	verdict OutlierVerdict,
	includeTests bool,
	params OutlierParams,
) (Finding, bool) {
	survivors, suppressions := suppressMembers(members, includeTests)
	partial := Finding{
		ID:           findingID(repoID, KindConventionOutlier, OutlierFingerprint(cohort.Source, cohort.Key, verdict.CalleeID)),
		RepoID:       repoID,
		Kind:         KindConventionOutlier,
		Fingerprint:  OutlierFingerprint(cohort.Source, cohort.Key, verdict.CalleeID),
		Suppressions: suppressions,
	}
	byID := make(map[string]Member, len(survivors))
	for _, member := range survivors {
		byID[member.EntityID] = member
	}
	callerIDs := make([]string, 0, len(verdict.CallerIDs))
	for _, id := range verdict.CallerIDs {
		if _, ok := byID[id]; ok {
			callerIDs = append(callerIDs, id)
		}
	}
	outlierIDs := make([]string, 0, len(verdict.OutlierIDs))
	for _, id := range verdict.OutlierIDs {
		if _, ok := byID[id]; ok {
			outlierIDs = append(outlierIDs, id)
		}
	}
	sort.Strings(callerIDs)
	sort.Strings(outlierIDs)
	share := 0.0
	if len(survivors) > 0 {
		share = float64(len(callerIDs)) / float64(len(survivors))
	}
	if len(survivors) < 2 || len(callerIDs) == 0 || len(outlierIDs) == 0 || share < params.MinShare {
		partial.Suppressions[RuleOutlierUnqualified] += len(survivors)
		return partial, false
	}
	confidence := 0.0
	inferred := false
	first := true
	for _, caller := range callerIDs {
		for _, edge := range edges[caller] {
			if edge.CalleeID != verdict.CalleeID {
				continue
			}
			if first || edge.EdgeConfidence < confidence {
				confidence = edge.EdgeConfidence
				first = false
			}
			if codeprovenance.IsInferred(codeprovenance.Method(edge.EdgeMethod)) {
				inferred = true
			}
		}
	}
	mediated := make([]OutlierMediation, 0, len(verdict.Mediated))
	for _, mediation := range verdict.Mediated {
		if _, ok := byID[mediation.OutlierID]; ok {
			mediated = append(mediated, mediation)
		}
	}
	tokenCount := 0
	for _, member := range survivors {
		if member.TokenCount > tokenCount {
			tokenCount = member.TokenCount
		}
	}
	ordered := make([]Member, 0, len(survivors))
	for _, id := range outlierIDs {
		ordered = append(ordered, byID[id])
	}
	for _, id := range callerIDs {
		ordered = append(ordered, byID[id])
	}
	truncated := cohort.TotalMembers > len(cohort.Members)
	detail := &OutlierDetail{
		Source:      cohort.Source,
		Key:         cohort.Key,
		Label:       cohort.Label,
		CohortSize:  len(survivors),
		Truncated:   truncated,
		TotalCohort: cohort.TotalMembers,
		CalleeID:    verdict.CalleeID,
		CalleeName:  verdict.CalleeName,
		Share:       share,
		CallerIDs:   callerIDs,
		OutlierIDs:  outlierIDs,
		Confidence:  confidence,
		Inferred:    inferred,
		Mediated:    mediated,
	}
	if len(mediated) == 0 {
		detail.Mediated = nil
	}
	return Finding{
		ID:           partial.ID,
		RepoID:       repoID,
		Kind:         KindConventionOutlier,
		Fingerprint:  partial.Fingerprint,
		Members:      ordered,
		Reasons:      buildOutlierReasons(cohort, detail, len(ordered), tokenCount),
		Score:        len(ordered) * tokenCount,
		Confidence:   confidence,
		Suppressions: partial.Suppressions,
		Outlier:      detail,
	}, true
}

// buildOutlierReasons decomposes members × tokens without remainder: one
// majority reason naming the callee, the cohort, the share, and the
// weakest-edge confidence, plus one missing-call reason per extra member.
// Cohort source, inferred-edge ambiguity, wrapper mediation, and truncation
// ride as zero-weight signals.
func buildOutlierReasons(cohort OutlierCohort, detail *OutlierDetail, memberCount, tokenCount int) []Reason {
	reasons := []Reason{{
		Code: ReasonOutlierMajority,
		Sentence: fmt.Sprintf(
			"%d of %d %s-cohort members call %s (%s) at share %.2f, weakest-edge confidence %.2f",
			len(detail.CallerIDs), memberCount, string(cohort.Source),
			detail.CalleeName, detail.CalleeID, detail.Share, detail.Confidence,
		),
		Value: tokenCount,
	}}
	for i := 1; i < memberCount; i++ {
		reasons = append(reasons, Reason{
			Code:     ReasonOutlierMissing,
			Sentence: fmt.Sprintf("cohort member %d of %d does not call %s (%d tokens)", i+1, memberCount, detail.CalleeID, tokenCount),
			Value:    tokenCount,
		})
	}
	reasons = append(reasons, Reason{
		Code:     ReasonOutlierCohort,
		Sentence: fmt.Sprintf("cohort %s %q holds %d analyzed members (judgment signal, no score weight)", string(cohort.Source), cohort.Label, memberCount),
		Value:    0,
	})
	if detail.Inferred {
		reasons = append(reasons, Reason{
			Code:     ReasonOutlierInferred,
			Sentence: "the majority rests on at least one inferred CALLS edge: confirm the callee binding before acting (judgment signal, no score weight)",
			Value:    0,
		})
	}
	for _, mediation := range detail.Mediated {
		reasons = append(reasons, Reason{
			Code: ReasonOutlierMediated,
			Sentence: fmt.Sprintf(
				"outlier %s reaches %s through %s (%s): verify whether it is the canonical wrapper on the wrapper_bypass surface (judgment signal, no score weight)",
				mediation.OutlierID, detail.CalleeID, mediation.MediatorName, mediation.MediatorID,
			),
			Value: 0,
		})
	}
	if detail.Truncated {
		reasons = append(reasons, Reason{
			Code:     ReasonOutlierTruncated,
			Sentence: fmt.Sprintf("cohort truncated to %d of %d members for bounded analysis (judgment signal, no score weight)", len(cohort.Members), cohort.TotalMembers),
			Value:    0,
		})
	}
	return reasons
}
