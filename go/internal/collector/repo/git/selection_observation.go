// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// SelectionObserver records one #7625 repository selection evaluation: the
// categorized listing one selector observed this cycle. Like
// ReindexWatermarkReader it is a narrow port the git package defines and the
// Postgres store implements, so this package never imports storage. A nil
// observer disables evaluation; filesystem, webhook, and bootstrap paths
// never evaluate.
type SelectionObserver interface {
	RecordSelectionEvaluation(ctx context.Context, evaluation scope.SelectionEvaluation) (scope.SelectionEvaluationOutcome, error)
}

// selectionTokenHashSalt separates the credential-identity hash from every
// other token digest: the same token hashes differently here than anywhere
// else, so a leaked selector id cannot be matched against another system's
// token hashes.
const selectionTokenHashSalt = "eshu-selection-observer-v1"

// observeRepositorySelection evaluates one successful discovery for #7625.
// The caller gates it to shard 0 with the full pre-shard listing; this
// helper gates it to the githubOrg and explicit modes. Explicit mode writes
// positive selected rows for its configured repositories; every other mode
// returns without recording.
//
// A truncated githubOrg listing is skipped (outcome=listing_truncated): an
// incomplete list would mark present repositories as missing. A store error
// is a WARN, never fatal to ingestion. On a recorded evaluation the helper
// emits the evaluation counter, refreshes the per-state scopes gauge, and
// logs git_repository_selection_evaluated; a tripped guard, a liveness gap
// past the window, a truncation, and a store error each log their own WARN.
func observeRepositorySelection(
	ctx context.Context,
	config RepoSyncConfig,
	token string,
	selection RepositorySelection,
	observedAt time.Time,
	observer SelectionObserver,
	logger *slog.Logger,
	inst *telemetry.Instruments,
) {
	if observer == nil {
		return
	}
	var kind string
	switch strings.TrimSpace(config.SourceMode) {
	case "githubOrg":
		kind = scope.SelectionSelectorKindGitHubOrg
	case "explicit":
		kind = scope.SelectionSelectorKindExplicit
	default:
		return
	}
	observedAt = observedAt.UTC()
	if kind == scope.SelectionSelectorKindGitHubOrg && selection.ListingTruncated {
		recordSelectionEvaluationOutcome(ctx, inst, scope.SelectionEvaluationListingTruncated, kind)
		if logger != nil {
			logger.WarnContext(ctx, "git_repository_selection_listing_truncated",
				slog.String("selector_kind", kind),
				slog.Int("listed_count", len(selection.RepositoryIDs)+len(selection.ArchivedRepositoryIDs)+len(selection.RuleExcludedRepositoryIDs)),
				slog.Int("repo_limit", config.RepoLimit))
		}
		return
	}

	evaluation := buildSelectionEvaluation(config, token, selection, observedAt, kind)
	outcome, err := observer.RecordSelectionEvaluation(ctx, evaluation)
	if err != nil {
		recordSelectionEvaluationOutcome(ctx, inst, scope.SelectionEvaluationStoreError, kind)
		if logger != nil {
			logger.WarnContext(ctx, "git_repository_selection_store_error",
				slog.String("selector_kind", kind),
				slog.String("selector_id", evaluation.SelectorID),
				log.Err(err))
		}
		return
	}
	recorded := outcome.Outcome
	switch recorded {
	case scope.SelectionEvaluationEvaluated, scope.SelectionEvaluationGuardTripped:
	default:
		// The store speaks evaluated|guard_tripped; anything else is a
		// version-skew bug. The counter keeps its closed vocabulary and
		// the WARN below carries the raw value for diagnosis.
		recorded = scope.SelectionEvaluationStoreError
	}
	recordSelectionEvaluationOutcome(ctx, inst, recorded, kind)
	switch recorded {
	case scope.SelectionEvaluationGuardTripped:
		if logger != nil {
			logger.WarnContext(ctx, "git_repository_selection_guard_tripped",
				slog.String("selector_kind", kind),
				slog.String("selector_id", evaluation.SelectorID),
				slog.Int("known_scope_count", outcome.KnownScopes),
				slog.Int("newly_missing_count", outcome.NewlyMissing))
		}
		return
	case scope.SelectionEvaluationEvaluated:
		recordSelectionScopesGauge(ctx, inst, outcome)
		if logger != nil {
			logger.InfoContext(ctx, "git_repository_selection_evaluated",
				slog.String("selector_kind", kind),
				slog.String("selector_id", evaluation.SelectorID),
				slog.Int("selected_count", outcome.Selected),
				slog.Int("archived_excluded_count", outcome.ArchivedExcluded),
				slog.Int("rule_excluded_count", outcome.RuleExcluded),
				slog.Int("not_listed_count", outcome.NotListed))
		}
		if !outcome.PriorEvaluatedAt.IsZero() && observedAt.Sub(outcome.PriorEvaluatedAt.UTC()) > evaluationWindow(config) {
			if logger != nil {
				logger.WarnContext(ctx, "git_repository_selection_liveness_lapsed",
					slog.String("selector_kind", kind),
					slog.String("selector_id", evaluation.SelectorID),
					slog.Time("prior_evaluated_at", outcome.PriorEvaluatedAt.UTC()),
					slog.Time("evaluated_at", observedAt))
			}
		}
	default:
		if logger != nil {
			logger.WarnContext(ctx, "git_repository_selection_store_error",
				slog.String("selector_kind", kind),
				slog.String("selector_id", evaluation.SelectorID),
				slog.String("outcome", outcome.Outcome))
		}
	}
}

// buildSelectionEvaluation maps one discovery into the store's evaluation
// shape: the selector id, the categorized scope IDs, and the cycle stamp.
// Repositories that map to no scope (an invalid checkout name) are dropped;
// they cannot sync either, so the store must not judge them missing.
func buildSelectionEvaluation(
	config RepoSyncConfig,
	token string,
	selection RepositorySelection,
	observedAt time.Time,
	kind string,
) scope.SelectionEvaluation {
	evaluation := scope.SelectionEvaluation{
		SelectorID:            selectionSelectorID(config, token),
		SelectorKind:          kind,
		EvaluatedAt:           observedAt,
		LivenessWindowSeconds: int(evaluationWindow(config) / time.Second),
	}
	if kind == scope.SelectionSelectorKindExplicit {
		for _, repoID := range selection.RepositoryIDs {
			if scopeID := selectionScopeIDForRepo(config, repoID); scopeID != "" {
				evaluation.Listed = append(evaluation.Listed, scope.EvaluatedRepository{ScopeID: scopeID})
			}
		}
		return evaluation
	}
	evaluation.Org = strings.ToLower(strings.TrimSpace(config.GithubOrg))
	for _, repoID := range selection.RepositoryIDs {
		if scopeID := selectionScopeIDForRepo(config, repoID); scopeID != "" {
			evaluation.Listed = append(evaluation.Listed, scope.EvaluatedRepository{
				ScopeID:  scopeID,
				GitHubID: selection.GitHubIDsByRepoID[normalizeRepositoryID(repoID)],
			})
		}
	}
	for _, repoID := range selection.ArchivedRepositoryIDs {
		if scopeID := selectionScopeIDForRepo(config, repoID); scopeID != "" {
			evaluation.Archived = append(evaluation.Archived, scope.EvaluatedRepository{
				ScopeID:  scopeID,
				GitHubID: selection.GitHubIDsByRepoID[normalizeRepositoryID(repoID)],
			})
		}
	}
	for _, repoID := range selection.RuleExcludedRepositoryIDs {
		if scopeID := selectionScopeIDForRepo(config, repoID); scopeID != "" {
			evaluation.RuleExcluded = append(evaluation.RuleExcluded, scope.EvaluatedRepository{
				ScopeID:  scopeID,
				GitHubID: selection.GitHubIDsByRepoID[normalizeRepositoryID(repoID)],
			})
		}
	}
	return evaluation
}

// evaluationWindow returns the configured selection liveness window, or the
// default when the config carries none (hand-built configs in tests).
func evaluationWindow(config RepoSyncConfig) time.Duration {
	if config.SelectionLivenessWindow <= 0 {
		return DefaultSelectionLivenessWindow
	}
	return config.SelectionLivenessWindow
}

// selectionSelectorID hashes the mode, org, rules, archived setting, and
// credential identity into one stable selector id. Two collectors that
// could list different repositories must never share one: otherwise one's
// listing would confirm or clear the other's exclusions.
func selectionSelectorID(config RepoSyncConfig, token string) string {
	parts := []string{
		strings.TrimSpace(config.SourceMode),
		strings.ToLower(strings.TrimSpace(config.GithubOrg)),
		fmt.Sprintf("archived=%t", config.IncludeArchivedRepos),
		selectionCredentialIdentity(config, token),
	}
	rules := make([]string, 0, len(config.RepositoryRules))
	for _, rule := range config.RepositoryRules {
		rules = append(rules, strings.ToLower(strings.TrimSpace(rule.Kind))+"="+strings.TrimSpace(rule.Value))
	}
	sort.Strings(rules)
	parts = append(parts, rules...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sel_" + hex.EncodeToString(sum[:])[:32]
}

// selectionCredentialIdentity names the credential behind a listing without
// ever carrying the credential itself: app:<app_id>:<installation_id> for
// GitHub App auth, a salted truncated token hash for token auth, or "none"
// when the mode lists without a credential. A rotation changes the id, so
// observations from before the rotation lapse instead of mixing listings
// two credentials saw.
func selectionCredentialIdentity(config RepoSyncConfig, token string) string {
	appID := strings.TrimSpace(config.GitHubAppID)
	installation := strings.TrimSpace(config.GitHubAppInstallation)
	if appID != "" && installation != "" {
		return "app:" + appID + ":" + installation
	}
	if strings.TrimSpace(token) == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(selectionTokenHashSalt + "\x00" + token))
	return "token:" + hex.EncodeToString(sum[:])[:16]
}

// selectionScopeIDForRepo derives the ingestion scope ID for one selected
// repository ID through the same remote-URL identity the commit path uses
// (repoRemoteURL, then the canonical repository id, then the
// git-repository-scope prefix). It returns "" when the repository cannot
// map to a scope.
func selectionScopeIDForRepo(config RepoSyncConfig, repoID string) string {
	normalized := normalizeRepositoryID(repoID)
	if normalized == "" {
		return ""
	}
	checkoutName, err := repoCheckoutName(normalized)
	if err != nil {
		return ""
	}
	remoteURL := repoRemoteURL(config, normalized)
	if strings.TrimSpace(remoteURL) == "" {
		return ""
	}
	metadata, err := repositoryidentity.MetadataFor(checkoutName, "", remoteURL)
	if err != nil {
		return ""
	}
	return buildScope(metadata, "").ScopeID
}

// recordSelectionEvaluationOutcome counts one evaluation attempt by outcome
// and selector kind. It tolerates a nil Instruments: an unwired observer
// still records.
func recordSelectionEvaluationOutcome(
	ctx context.Context,
	inst *telemetry.Instruments,
	outcome string,
	kind string,
) {
	if inst == nil || inst.RepositorySelectionEvaluations == nil {
		return
	}
	inst.RepositorySelectionEvaluations.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(outcome), telemetry.AttrSelectorKind(kind)))
}

// recordSelectionScopesGauge refreshes the per-state scopes gauge from one
// recorded evaluation, zeros included. It runs only on outcome=evaluated,
// so a guard trip, truncation, or store error leaves the previous reading.
func recordSelectionScopesGauge(
	ctx context.Context,
	inst *telemetry.Instruments,
	outcome scope.SelectionEvaluationOutcome,
) {
	if inst == nil || inst.RepositorySelectionScopes == nil {
		return
	}
	inst.RepositorySelectionScopes.Record(ctx, int64(outcome.Selected), metric.WithAttributes(telemetry.AttrSelectionState(scope.SelectionStateSelected)))
	inst.RepositorySelectionScopes.Record(ctx, int64(outcome.ArchivedExcluded), metric.WithAttributes(telemetry.AttrSelectionState(scope.SelectionStateArchivedExcluded)))
	inst.RepositorySelectionScopes.Record(ctx, int64(outcome.RuleExcluded), metric.WithAttributes(telemetry.AttrSelectionState(scope.SelectionStateRuleExcluded)))
	inst.RepositorySelectionScopes.Record(ctx, int64(outcome.NotListed), metric.WithAttributes(telemetry.AttrSelectionState(scope.SelectionStateNotListed)))
}
