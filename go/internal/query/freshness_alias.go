// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: type aliases and thin forwarders for the moved freshness family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"github.com/eshu-hq/eshu/go/internal/query/freshness"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// freshness_alias.go is the root alias shim for the freshness handler family
// (#6642, modelled on language_alias.go and family_codeowners_shim.go). Its
// method files moved to freshness/. Names the rest of the program still
// spells `query.X` (handler.go's struct field, cmd router wiring,
// auth_scoped_routes.go, status_freshness_causality.go, metrics.go,
// answer_packet.go, and staying root tests) alias here so the move touches
// no caller outside the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import freshness directly.

// FreshnessHandler is the freshness-drilldown handler family type. Its home
// is freshness/; this alias keeps handler.go's `Freshness *FreshnessHandler`
// field, cmd/api's and cmd/mcp-server's wiring, and staying root tests
// (auth_all_scope_bearer_two_tenant_test.go, service_changed_since_grant_test.go)
// spelling query.FreshnessHandler unchanged.
type FreshnessHandler = freshness.Handler

// FreshnessCause is the closed reason a truth response is not fresh. Its
// home is freshness/ (itself an alias of querycontract.FreshnessCause);
// answer_packet.go, metrics.go, and querycontract_boundary_test.go keep
// spelling query.FreshnessCause unchanged.
type FreshnessCause = freshness.Cause

// FreshnessNextCheck is a bounded follow-up call for one freshness cause.
// querycontract_boundary_test.go keeps spelling query.FreshnessNextCheck
// unchanged.
type FreshnessNextCheck = freshness.NextCheck

// FreshnessCausality is the operator read model for stale-answer and graph
// retraction causality. Its home is freshness/; status_freshness_causality.go
// (StatusHandler.getFreshnessCausality) keeps using it as a bare identifier
// through this alias.
type FreshnessCausality = freshness.Causality

// FreshnessCauseStatus is one closed cause, whether it is currently
// observed, how it can be observed, and its bounded drilldown. Its home is
// freshness/; status_freshness_causality.go keeps using it unchanged.
type FreshnessCauseStatus = freshness.CauseStatus

// FreshnessGenerations summarizes the generation lifecycle. Its home is
// freshness/; status_freshness_causality.go keeps using it unchanged.
type FreshnessGenerations = freshness.Generations

// FreshnessPendingProjection summarizes projection work still owed before
// the graph catches up to the active generation. Its home is freshness/;
// status_freshness_causality.go keeps using it unchanged.
type FreshnessPendingProjection = freshness.PendingProjection

// FreshnessTransition is one recent generation lifecycle row. Its home is
// freshness/; status_freshness_causality.go keeps using it unchanged.
type FreshnessTransition = freshness.Transition

// GenerationLifecycleReader reads one bounded, ordered page of scope
// generation lifecycle drilldown rows. Its home is freshness/; cmd/api's and
// cmd/mcp-server's wiring keep spelling query.GenerationLifecycleReader
// unchanged.
type GenerationLifecycleReader = freshness.GenerationLifecycleReader

// ChangedSinceReader computes one bounded changed-since delta summary. Its
// home is freshness/; cmd/api's and cmd/mcp-server's wiring keep spelling
// query.ChangedSinceReader unchanged.
type ChangedSinceReader = freshness.ChangedSinceReader

// ServiceChangedSinceReader computes one bounded service-scope
// changed-since delta summary. Its home is freshness/; cmd/api's and
// cmd/mcp-server's wiring keep spelling query.ServiceChangedSinceReader
// unchanged.
type ServiceChangedSinceReader = freshness.ServiceChangedSinceReader

// Freshness cause aliases preserve every pre-move root spelling of the
// closed cause enumeration. The first four still have callers (metrics.go,
// answer_packet_test.go, internal/mcp/summaries_test.go,
// internal/answerquality/report_corpus.go and
// internal/serviceintel's suggestions test); the last four are caller-free
// today and stay so the alias stanza keeps the whole enumeration, not a
// subset a future caller would find half-missing.
const (
	FreshnessCauseReducerBacklog             = freshness.CauseReducerBacklog
	FreshnessCauseMissingCollectorCompletion = freshness.CauseMissingCollectorCompletion
	FreshnessCauseContentCoverageUnavailable = freshness.CauseContentCoverageUnavailable
	FreshnessCauseDeadLetteredDomain         = freshness.CauseDeadLetteredDomain
	FreshnessCausePendingRepoGeneration      = freshness.CausePendingRepoGeneration
	FreshnessCauseUnsupportedProfile         = freshness.CauseUnsupportedProfile
	FreshnessCauseRetentionExpired           = freshness.CauseRetentionExpired
	FreshnessCausePendingSearchVector        = freshness.CausePendingSearchVector
)

// WithFreshnessCause attaches a proven cause to a non-fresh envelope. Its
// home is freshness/ (WithCause); metrics.go, answer_packet_test.go, and
// internal/mcp/summaries_test.go keep spelling query.WithFreshnessCause
// unchanged.
func WithFreshnessCause(truth *querycontract.TruthEnvelope, cause FreshnessCause) {
	freshness.WithCause(truth, cause)
}

// ValidFreshnessCause reports whether cause belongs to the closed
// enumeration. Its home is freshness/ (ValidCause);
// internal/serviceintel/suggestions.go keeps spelling
// query.ValidFreshnessCause unchanged.
func ValidFreshnessCause(cause FreshnessCause) bool {
	return freshness.ValidCause(cause)
}

// FreshnessCauseNextCheck returns the bounded follow-up call for a cause and
// whether the cause has one. Its home is freshness/ (CauseNextCheck); it has
// no caller outside the family today and is kept so root's pre-move exported
// surface stays whole.
func FreshnessCauseNextCheck(cause FreshnessCause) (FreshnessNextCheck, bool) {
	return freshness.CauseNextCheck(cause)
}

// freshnessCausalityFromRawAndReport forwards to freshness.CausalityFromRawAndReport.
// Its home is freshness/, exported there because
// status_freshness_causality.go (StatusHandler.getFreshnessCausality, not
// moved -- it is a StatusHandler method and scopedFreshnessCausalityRoute is
// read by auth_scoped_routes.go) is the caller that needs it and must not
// import freshness directly into its own identifier spelling.
func freshnessCausalityFromRawAndReport(raw status.RawSnapshot, report status.Report) FreshnessCausality {
	return freshness.CausalityFromRawAndReport(raw, report)
}

// freshnessNextCheckAsRecommendedCall forwards to
// freshness.NextCheckAsRecommendedCall. Its home is freshness/, exported
// there because status_freshness_causality.go's freshnessCausesToSlice is
// the caller that needs it.
func freshnessNextCheckAsRecommendedCall(next FreshnessNextCheck) map[string]any {
	return freshness.NextCheckAsRecommendedCall(next)
}
