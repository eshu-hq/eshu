// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

// UpstreamClassification is the probe verdict for a Confirmed not_listed
// scope. It aliases selection.UpstreamClassification so the writer and the
// freshness reader share one classification set, the way State aliases
// selection.State.
type UpstreamClassification = selection.UpstreamClassification

// The probe verdicts, re-exported from the selection package.
const (
	UpstreamTransferred            = selection.UpstreamTransferred
	UpstreamRenamed                = selection.UpstreamRenamed
	UpstreamStillPresent           = selection.UpstreamStillPresent
	UpstreamNotFoundOrInaccessible = selection.UpstreamNotFoundOrInaccessible
)

// ProbeCapPerCycle bounds the upstream probes of one selector evaluation
// (#8454): at most 25 repositories per cycle, oldest state_since first, so a
// large unlisted backlog drains across cycles instead of in one burst.
const ProbeCapPerCycle = 25

// ProbeTarget is one Confirmed not_listed scope the probe may classify.
// PriorClassification and PriorSuccessor carry the row's stored verdict so
// an inconclusive probe keeps them; ClassifiedAt gates the re-probe.
type ProbeTarget struct {
	ScopeID             string
	Slug                string
	GitHubRepoID        int64
	SelectorOwner       string
	StateSince          time.Time
	PriorClassification UpstreamClassification
	PriorSuccessor      string
	ClassifiedAt        time.Time
	LivenessWindow      time.Duration
}

// ProbeResult is one upstream probe answer. Found with a FullName is a 200
// carrying the repository's current full_name; NotFound is a 404, which
// never reads "deleted". The zero value is inconclusive: a 5xx, a
// rate-limit, or a network or decode failure, which keeps the prior
// classification and never fails ingestion.
type ProbeResult struct {
	Found    bool
	FullName string
	NotFound bool
}

// ProbeFunc answers one target's upstream state. An error is inconclusive,
// like a zero ProbeResult: the target keeps its prior classification.
type ProbeFunc func(ctx context.Context, target ProbeTarget) (ProbeResult, error)

// ClassificationRow is one probe verdict the store writes: the scope's new
// upstream classification and successor slug. Rows whose probe was
// inconclusive never reach the store.
type ClassificationRow struct {
	ScopeID        string
	Classification UpstreamClassification
	SuccessorSlug  string
}

// SelectProbeTargets returns the Confirmed not_listed observations the next
// probe pass may classify: oldest state_since first, at most
// ProbeCapPerCycle. A row is eligible only when Confirmed says its exclusion
// is settled, when known names its slug (without the slug neither the
// /repos fallback nor the same-name comparison is possible), and when it has
// never been classified or its classification is older than its liveness
// window, so a scope re-probes at most once per window. The caller probes
// github_org selectors only; gitlab_group and explicit rows never reach
// here.
func SelectProbeTargets(prior []Observation, known map[string]KnownScope, owner string, now time.Time) []ProbeTarget {
	var targets []ProbeTarget
	for _, observation := range prior {
		if observation.State != StateNotListed || !Confirmed(observation) {
			continue
		}
		scope, ok := known[observation.ScopeID]
		if !ok || strings.TrimSpace(scope.Slug) == "" {
			continue
		}
		window := observation.LivenessWindow
		if window <= 0 {
			window = DefaultLivenessWindow
		}
		if !observation.ClassifiedAt.IsZero() && !observation.ClassifiedAt.Add(window).Before(now) {
			continue
		}
		targets = append(targets, ProbeTarget{
			ScopeID:             observation.ScopeID,
			Slug:                scope.Slug,
			GitHubRepoID:        observation.GitHubRepoID,
			SelectorOwner:       owner,
			StateSince:          observation.StateSince,
			PriorClassification: observation.Classification,
			PriorSuccessor:      observation.SuccessorSlug,
			ClassifiedAt:        observation.ClassifiedAt,
			LivenessWindow:      window,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].StateSince.Equal(targets[j].StateSince) {
			return targets[i].ScopeID < targets[j].ScopeID
		}
		return targets[i].StateSince.Before(targets[j].StateSince)
	})
	if len(targets) > ProbeCapPerCycle {
		targets = targets[:ProbeCapPerCycle]
	}
	return targets
}

// Classify maps one probe answer to the scope's upstream classification. It
// returns the classification, the successor slug (only for renamed and
// transferred), and whether the store should write: an inconclusive probe
// keeps the prior verdict and writes nothing. A 200 whose full_name matches
// the scope slug is still_present: the listing missed a present repository.
// A 200 with a different name is renamed under the same owner and
// transferred otherwise; whether the successor resolves to a selected scope
// is decided at read time only, so renamed covers both cases here.
func Classify(target ProbeTarget, result ProbeResult) (UpstreamClassification, string, bool) {
	switch {
	case result.NotFound:
		return UpstreamNotFoundOrInaccessible, "", true
	case !result.Found:
		return target.PriorClassification, target.PriorSuccessor, false
	}
	fullName := strings.TrimSpace(result.FullName)
	if fullName == "" {
		return target.PriorClassification, target.PriorSuccessor, false
	}
	if strings.EqualFold(fullName, strings.TrimSpace(target.Slug)) {
		return UpstreamStillPresent, "", true
	}
	if owner, _, found := strings.Cut(fullName, "/"); found && strings.EqualFold(strings.TrimSpace(owner), strings.TrimSpace(target.SelectorOwner)) {
		return UpstreamRenamed, fullName, true
	}
	return UpstreamTransferred, fullName, true
}

// validUpstreamClassification reports whether c is a storable probe verdict:
// one of the four CHECK values. Empty means unclassified and is never
// written; anything else is rejected before any write.
func validUpstreamClassification(c UpstreamClassification) bool {
	switch c {
	case UpstreamTransferred, UpstreamRenamed, UpstreamStillPresent, UpstreamNotFoundOrInaccessible:
		return true
	default:
		return false
	}
}
