// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
)

// Security-alert collection-state truth for an empty answer (#8803).
//
// A zero-row security-alert page used to report coverage.state=complete with an
// exact, fresh truth envelope even when the collector had never collected the
// requested repository. This file classifies the collector-side state behind an
// empty answer from three facts the SecurityAlertCollectionStateStore reads:
// the enabled instances' target configuration, each target's latest work item,
// and whether the target has any committed generation. The classification is a
// closed set of coverage states and reason tokens; it never carries a target
// scope id, a repository name, an allowlist, a URL, or a failure message.

// Coverage states beyond the row-derived complete and target_incomplete.
const (
	// securityAlertCoverageNotConfigured means no enabled target covers the
	// requested scope, so an empty answer says nothing about the repository.
	securityAlertCoverageNotConfigured = "not_configured"
)

// Coverage reason tokens. The set is closed and mirrored by the OpenAPI enum on
// coverage.reasons; securityAlertCoverageReasons lists every value.
const (
	securityAlertReasonCollectorNotConfigured     = "collector_not_configured"
	securityAlertReasonRepositoryOutsideTargets   = "repository_outside_target_set"
	securityAlertReasonNoCommittedGeneration      = "no_committed_generation"
	securityAlertReasonTerminalFetchFailurePrefix = "terminal_fetch_failure:"
	securityAlertReasonCollectorStateUnavailable  = "collector_state_unavailable"
	// securityAlertReasonNoAuthorizedRepositories marks the empty answer a
	// scoped token with no grants gets: it authorizes no repository, so no
	// enabled target covers the requested scope for that caller.
	securityAlertReasonNoAuthorizedRepositories = "no_authorized_repositories"
)

// securityAlertTerminalFailureClasses is every failure class the security-alert
// claim runner can persist on a terminal work item. Any other stored value
// maps to "unclassified" so free text never reaches a payload, label, or log.
// The literals are pinned here, not imported, because query must not depend
// on the collector packages. Sources:
//
//   - auth_denied, not_found, rate_limited, terminal, retryable: the provider
//     classes (go/internal/collector/sdk/failure.go, mapped by
//     collector/securityalerts/alertruntime/failure.go). failTerminal takes
//     the class from the error chain first
//     (collector/claimed_service_validation.go), and budgetExhaustedError
//     wraps the cause with %w (collector/claimed_service.go), so a retry
//     budget exhausted on a transient provider failure persists retryable on
//     a terminal row.
//   - attempt_budget_exhausted: the budget class when the cause carries no
//     class (FailureClassAttemptBudgetExhausted, collector/claimed_service.go).
//   - identity_mismatch: a claim whose collected identity does not match
//     (collector/claimed_service.go, the FailClaimTerminal calls).
//   - collect_failure, commit_failure: the claim-stage fallbacks for a
//     non-retryable collect or commit error (collector/claimed_service.go).
var securityAlertTerminalFailureClasses = []string{
	"auth_denied", "not_found", "rate_limited", "terminal", "retryable",
	"attempt_budget_exhausted", "identity_mismatch", "collect_failure", "commit_failure",
}

// securityAlertCoverageReasons is every reason token a response may carry. The
// OpenAPI enum test compares the published enum to this list.
func securityAlertCoverageReasons() []string {
	out := []string{
		securityAlertReasonCollectorNotConfigured,
		securityAlertReasonRepositoryOutsideTargets,
		securityAlertReasonNoCommittedGeneration,
		securityAlertReasonCollectorStateUnavailable,
		securityAlertReasonNoAuthorizedRepositories,
	}
	out = append(out, securityAlertUnreadReasons...)
	for _, class := range securityAlertTerminalFailureClasses {
		out = append(out, securityAlertReasonTerminalFetchFailurePrefix+class)
	}
	out = append(out, securityAlertReasonTerminalFetchFailurePrefix+"unclassified")
	slices.Sort(out)
	return out
}

// Collector states reported on the stage log and nowhere else. Bounded.
const (
	securityAlertCollectorStateNotConfigured    = "not_configured"
	securityAlertCollectorStateOutsideTargets   = "outside_target_set"
	securityAlertCollectorStateCollecting       = "collecting"
	securityAlertCollectorStateFailedTerminal   = "failed_terminal"
	securityAlertCollectorStateStaleAfterFailed = "stale_after_terminal_failure"
	securityAlertCollectorStateHealthy          = "healthy"
	securityAlertCollectorStatePartialRead      = "partial_read"
	securityAlertCollectorStateUnavailable      = "state_unavailable"
)

// securityAlertTargetScopeOrg is the scope value of an organization target.
// It is the token the collector and the workflow validator accept ("org");
// the decoder used to compare against "organization", which no accepted
// configuration carries, so no org target ever covered an anchored read.
const securityAlertTargetScopeOrg = "org"

// securityAlertProviderScopePrefix is the provider scope id prefix the
// repository selector emits for a GitHub repository slug.
const securityAlertProviderScopePrefix = "security-alert:github:"

// securityAlertTargetConfig is the query-side view of one collector target.
// It decodes only the fields needed for membership; credentials, URLs, and
// limits are never read.
type securityAlertTargetConfig struct {
	Scope               string   `json:"scope"`
	ScopeID             string   `json:"scope_id"`
	Repository          string   `json:"repository"`
	Organization        string   `json:"organization"`
	AllowedRepositories []string `json:"allowed_repositories"`
}

type securityAlertCollectorConfig struct {
	Targets []securityAlertTargetConfig `json:"targets"`
}

// decodeSecurityAlertTargets returns every usable target across the enabled
// instances' configurations. A configuration the collector itself would reject
// (bad JSON, missing scope_id) contributes no target, because that collector
// cannot collect it either.
func decodeSecurityAlertTargets(configs []string) []securityAlertTargetConfig {
	out := make([]securityAlertTargetConfig, 0, len(configs))
	for _, raw := range configs {
		var decoded securityAlertCollectorConfig
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			continue
		}
		for _, target := range decoded.Targets {
			target.ScopeID = strings.TrimSpace(target.ScopeID)
			if target.ScopeID == "" || shard.HasMarker(target.ScopeID) {
				// The collector refuses a scope_id with the reserved shard
				// marker (#9019), so it collects nothing for it.
				continue
			}
			target.Scope = strings.TrimSpace(target.Scope)
			if target.Scope == "" {
				target.Scope = "repository"
			}
			target.Repository = normalizeSecurityAlertRepository(target.Repository)
			target.Organization = shard.NormalizeOrganization(target.Organization)
			allowed := make([]string, 0, len(target.AllowedRepositories))
			for _, repo := range target.AllowedRepositories {
				if normalized := normalizeSecurityAlertRepository(repo); normalized != "" {
					allowed = append(allowed, normalized)
				}
			}
			target.AllowedRepositories = allowed
			out = append(out, expandSecurityAlertOrgTarget(target)...)
		}
	}
	return out
}

// expandSecurityAlertOrgTarget returns the scopes an org target is collected
// as (#9019): itself when shard.Partition gives one shard, otherwise one
// target per shard with the shard's scope id and its repositories, the same
// expansion the planner and the collector run. An anchored read is then
// covered by the requested repository's shard only, and an unanchored read
// takes every shard. A sharded target's base scope is retired, so it is not
// listed.
func expandSecurityAlertOrgTarget(target securityAlertTargetConfig) []securityAlertTargetConfig {
	if target.Scope != securityAlertTargetScopeOrg {
		return []securityAlertTargetConfig{target}
	}
	leaves := shard.Partition(target.ScopeID, target.Organization, target.AllowedRepositories)
	if len(leaves) == 1 {
		return []securityAlertTargetConfig{target}
	}
	out := make([]securityAlertTargetConfig, 0, len(leaves))
	for _, leaf := range leaves {
		shardTarget := target
		shardTarget.ScopeID = leaf.ScopeID
		shardTarget.AllowedRepositories = leaf.Repositories
		out = append(out, shardTarget)
	}
	return out
}

// normalizeSecurityAlertRepository is the shared owner/name normalization
// (shard.NormalizeRepository) the planner and the collector also use. The
// shard package is a stdlib-only leaf, so query still imports no collector
// package.
func normalizeSecurityAlertRepository(value string) string {
	return shard.NormalizeRepository(value)
}

// securityAlertRequestedSlugs extracts the owner/name slugs from the provider
// scope ids the repository selector resolved.
func securityAlertRequestedSlugs(repositoryScopeIDs []string) []string {
	out := make([]string, 0, len(repositoryScopeIDs))
	for _, scopeID := range repositoryScopeIDs {
		slug, ok := strings.CutPrefix(strings.TrimSpace(scopeID), securityAlertProviderScopePrefix)
		if !ok {
			continue
		}
		if normalized := normalizeSecurityAlertRepository(slug); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

// securityAlertTargetCovers reports whether target collects slug: a repository
// target names it, and an organization target holds it in its allowlist under
// the same owner (the collector drops every other repository from the org
// stream).
func securityAlertTargetCovers(target securityAlertTargetConfig, slug string) bool {
	switch target.Scope {
	case "repository":
		return target.Repository == slug
	case securityAlertTargetScopeOrg:
		owner, _, _ := strings.Cut(slug, "/")
		return owner == target.Organization && slices.Contains(target.AllowedRepositories, slug)
	default:
		return false
	}
}

// securityAlertTargetScopeIDs returns the sorted, unique scope ids of the
// targets that cover any requested slug, or of every target when the read has
// no repository anchor.
func securityAlertTargetScopeIDs(targets []securityAlertTargetConfig, slugs []string, anchored bool) []string {
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		if !anchored {
			out = append(out, target.ScopeID)
			continue
		}
		for _, slug := range slugs {
			if securityAlertTargetCovers(target, slug) {
				out = append(out, target.ScopeID)
				break
			}
		}
	}
	return UniqueSortedNonEmpty(out)
}

// securityAlertCoverageVerdict is the classification of one empty answer.
type securityAlertCoverageVerdict struct {
	coverageState  string
	reasons        []string
	level          querycontract.TruthLevel
	freshness      querycontract.FreshnessState
	collectorState string
	// readiness is the collector_readiness envelope state to attach, or ""
	// to leave the envelope off. It is empty for a still-collecting or
	// terminal-failure (target_incomplete) answer, a repository outside every
	// target, and a grantless answer, whose coverage already says why it is not
	// a configured-but-empty result. An outside-target answer has an enabled
	// instance, so the envelope's not_configured ("no enabled instance")
	// would be false for it. A failed state read is also target_incomplete but
	// carries readiness_unavailable, because the read itself failed.
	readiness querycontract.CollectorListReadinessState
	// cause reports whether the freshness gap has the proven
	// missing_collector_completion cause.
	cause bool
}

// healthy reports whether the verdict leaves the row-derived answer unchanged.
func (v securityAlertCoverageVerdict) healthy() bool {
	return v.collectorState == securityAlertCollectorStateHealthy
}

// securityAlertCollectorNotConfiguredVerdict is the verdict when no enabled
// instance decodes to a target. It is the only coverage verdict that attaches
// the not_configured readiness envelope, because that envelope state means no
// enabled instance of the feeding collector is registered.
func securityAlertCollectorNotConfiguredVerdict() securityAlertCoverageVerdict {
	return securityAlertCoverageVerdict{
		coverageState:  securityAlertCoverageNotConfigured,
		reasons:        []string{securityAlertReasonCollectorNotConfigured},
		level:          querycontract.TruthLevelFallback,
		freshness:      querycontract.FreshnessUnavailable,
		collectorState: securityAlertCollectorStateNotConfigured,
		readiness:      querycontract.CollectorListReadinessStateNotConfigured,
		cause:          true,
	}
}

// securityAlertOutsideTargetsVerdict is the verdict when an enabled instance
// exists but no target covers the requested repository. It carries no
// readiness envelope: the collector is configured, only target membership is
// missing, and coverage.reasons already names that.
func securityAlertOutsideTargetsVerdict() securityAlertCoverageVerdict {
	return securityAlertCoverageVerdict{
		coverageState:  securityAlertCoverageNotConfigured,
		reasons:        []string{securityAlertReasonRepositoryOutsideTargets},
		level:          querycontract.TruthLevelFallback,
		freshness:      querycontract.FreshnessUnavailable,
		collectorState: securityAlertCollectorStateOutsideTargets,
		cause:          true,
	}
}

// securityAlertNoAuthorizedRepositoriesVerdict is the verdict for a scoped
// token with no grants (#8803 F1). It is computed without a collector read and
// says nothing about collector health, so it carries no readiness envelope and
// no freshness cause; it only keeps the zero-row answer from reading as a
// vacuous complete over zero repositories.
func securityAlertNoAuthorizedRepositoriesVerdict() securityAlertCoverageVerdict {
	return securityAlertCoverageVerdict{
		coverageState:  securityAlertCoverageNotConfigured,
		reasons:        []string{securityAlertReasonNoAuthorizedRepositories},
		level:          querycontract.TruthLevelFallback,
		freshness:      querycontract.FreshnessUnavailable,
		collectorState: securityAlertCollectorStateNotConfigured,
	}
}

func securityAlertUnavailableVerdict() securityAlertCoverageVerdict {
	return securityAlertCoverageVerdict{
		coverageState:  securityAlertCoverageTargetIncomplete,
		reasons:        []string{securityAlertReasonCollectorStateUnavailable},
		level:          querycontract.TruthLevelFallback,
		freshness:      querycontract.FreshnessUnavailable,
		collectorState: securityAlertCollectorStateUnavailable,
		readiness:      querycontract.CollectorListReadinessStateReadinessUnavailable,
	}
}

func securityAlertHealthyVerdict() securityAlertCoverageVerdict {
	return securityAlertCoverageVerdict{
		coverageState:  securityAlertCoverageComplete,
		level:          querycontract.TruthLevelExact,
		freshness:      querycontract.FreshnessFresh,
		collectorState: securityAlertCollectorStateHealthy,
		readiness:      querycontract.CollectorListReadinessStateReadyZeroResults,
	}
}

// classifySecurityAlertTarget ranks one target's state. Higher ranks are
// closer to a complete answer: 5 healthy, 4 committed but the requested
// repositories were not all read in full (#8943), 3 stale after a terminal
// failure, 2 still collecting, 1 terminal failure with nothing ever committed.
func classifySecurityAlertTarget(state SecurityAlertTargetState, anchored bool) (int, securityAlertCoverageVerdict) {
	terminal := state.LatestFailureTerminal || state.LatestWorkItemStatus == "failed_terminal"
	incomplete := securityAlertCoverageVerdict{
		coverageState: securityAlertCoverageTargetIncomplete,
		cause:         true,
	}
	// A retirement generation (#9019) holds no data: until the scope's next
	// collection activates, it counts as nothing committed.
	committed := state.CommittedGeneration && !state.Retired
	switch {
	case committed && !terminal:
		if reasons := securityAlertUnreadReasonsFor(state, anchored); len(reasons) > 0 {
			return 4, securityAlertUnreadVerdict(reasons)
		}
		return 5, securityAlertHealthyVerdict()
	case committed:
		incomplete.reasons = []string{securityAlertTerminalReason(state.LatestFailureClass)}
		incomplete.level = querycontract.TruthLevelDerived
		incomplete.freshness = querycontract.FreshnessStale
		incomplete.collectorState = securityAlertCollectorStateStaleAfterFailed
		return 3, incomplete
	case !terminal:
		incomplete.reasons = []string{securityAlertReasonNoCommittedGeneration}
		incomplete.level = querycontract.TruthLevelDerived
		incomplete.freshness = querycontract.FreshnessBuilding
		incomplete.collectorState = securityAlertCollectorStateCollecting
		return 2, incomplete
	default:
		incomplete.reasons = []string{securityAlertTerminalReason(state.LatestFailureClass)}
		incomplete.level = querycontract.TruthLevelFallback
		incomplete.freshness = querycontract.FreshnessUnavailable
		incomplete.collectorState = securityAlertCollectorStateFailedTerminal
		return 1, incomplete
	}
}

// securityAlertTargetMissesCoveredSlug reports whether the state's latest
// committed generation did not collect a requested slug that a configured
// target with the state's scope id covers. A slug no such target covers is
// ignored: the target was never asked to collect it.
func securityAlertTargetMissesCoveredSlug(
	targets []securityAlertTargetConfig,
	state SecurityAlertTargetState,
	slugs []string,
) bool {
	if len(state.UncollectedRequested) == 0 {
		return false
	}
	for _, slug := range slugs {
		if !slices.Contains(state.UncollectedRequested, slug) {
			continue
		}
		for _, target := range targets {
			if target.ScopeID == state.ScopeID && securityAlertTargetCovers(target, slug) {
				return true
			}
		}
	}
	return false
}

func securityAlertTerminalReason(class string) string {
	class = strings.TrimSpace(class)
	if !slices.Contains(securityAlertTerminalFailureClasses, class) {
		class = "unclassified"
	}
	return securityAlertReasonTerminalFetchFailurePrefix + class
}

// classifySecurityAlertCollection turns the decoded targets and their states
// into one verdict. An anchored read takes the best covering target (one
// healthy target makes the repository complete); an unanchored read takes the
// worst target (every target must be healthy) and lists each distinct reason.
//
// For an anchored read, a target whose latest committed generation did not
// collect a requested repository the target now covers (#8838: an allowlist
// entry added, or a scope_id reused for another repository, after that
// generation) has no committed generation for this read, so it ranks as still
// collecting, or as a terminal failure that never collected.
func classifySecurityAlertCollection(
	targets []securityAlertTargetConfig,
	scopeIDs []string,
	states []SecurityAlertTargetState,
	slugs []string,
	anchored bool,
) securityAlertCoverageVerdict {
	byScope := make(map[string]SecurityAlertTargetState, len(states))
	for _, state := range states {
		if anchored && state.CommittedGeneration && securityAlertTargetMissesCoveredSlug(targets, state, slugs) {
			state.CommittedGeneration = false
		}
		byScope[state.ScopeID] = state
	}
	bestRank, worstRank := 0, 6
	var best, worst securityAlertCoverageVerdict
	var reasons []string
	for _, scopeID := range scopeIDs {
		rank, verdict := classifySecurityAlertTarget(byScope[scopeID], anchored)
		if rank > bestRank {
			bestRank, best = rank, verdict
		}
		if rank < worstRank {
			worstRank, worst = rank, verdict
		}
		reasons = append(reasons, verdict.reasons...)
	}
	if anchored {
		return best
	}
	worst.reasons = UniqueSortedNonEmpty(reasons)
	return worst
}
