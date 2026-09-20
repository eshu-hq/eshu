// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	querycodedivergence "github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Suppression-count keys for loader pipeline filters. These are not catalogue
// suppression rules but counted drops the read surface reports alongside the
// rule counts, so no filtering is silent: rows without a persisted shingle
// set (pre-#6837 payloads) and band pairs already claimed as equality
// findings by the #6836 surface.
const (
	FilterNoShingles        = "no_shingles"
	FilterEqualityDuplicate = "equality_duplicate"
	repoScopePrefix         = "repo:"
)

// FindingWriter persists admitted drifted pairs as durable
// reducer_code_drifted_finding facts. The writer must be idempotent by
// finding identity so reducer retries never duplicate a row, and must retire
// every prior generation's finding the current write superseded.
type FindingWriter interface {
	WriteDriftedFindings(ctx context.Context, write DriftedWrite) (DriftedWriteResult, error)
}

// DriftedWrite is the durable publication request for one code_drifted
// reducer intent: the admitted pairs plus the generation suppression totals
// the read surface reports verbatim. Suppressions merges rule verdicts
// (below_floor, member rule names, similarity_below_threshold) with loader
// pipeline filter counts (below_floor, no_shingles, equality_duplicate);
// every evaluated key is present even when zero so a quiet result is
// distinguishable from an unevaluated one.
type DriftedWrite struct {
	IntentID        string
	ScopeID         string
	GenerationID    string
	RepoID          string
	SourceSystem    string
	Cause           string
	Pairs           []AdmittedPair
	Suppressions    map[string]int
	BudgetExhausted []string
}

// DriftedWriteResult reports the durable-write outcome.
type DriftedWriteResult struct {
	// Written counts the finding rows the current pass published.
	Written int
}

// CodeDriftedHandler materializes drifted parallel-implementation findings
// for one reducer intent: it loads the LSH-nominated candidate pairs for
// the intent's repo, verifies each by exact Jaccard over the persisted
// shingle sets, runs the intentional-parallel suppression catalogue (plus
// the drift-specific threshold reason), and hands the admitted pairs to the
// writer as durable facts.
type CodeDriftedHandler struct {
	// Loader supplies the within-budget candidate pairs for one repo. May
	// be nil; the handler then returns success without drift (no observable
	// input).
	Loader CandidateLoader
	// Writer persists admitted pairs and retires superseded generations.
	// May be nil; the handler then keeps counter+log-only behavior and does
	// not publish a durable read model.
	Writer FindingWriter
	// Instruments holds the drift counters. May be nil; the handler then
	// skips telemetry but still classifies for the structured log.
	Instruments *telemetry.Instruments
	// Logger receives the structured logs the handler emits per intent. May
	// be nil; the handler then drops logs.
	Logger *slog.Logger
}

// Handle executes the drifted-pair pipeline for one reducer intent. Loader
// failures return an error (queue retry): success with zero pairs would
// retire live findings on a transient database failure. Non-fatal
// rejections (foreign scope shape, missing repo, no loader) return success
// with a structured log: they are operator-actionable, not runtime failures.
func (h CodeDriftedHandler) Handle(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainCodeDrifted {
		return reducercontract.Result{}, fmt.Errorf(
			"code drifted handler does not accept domain %q",
			intent.Domain,
		)
	}
	repoID := driftedRepoID(intent)
	if repoID == "" {
		h.log(ctx, intent, "no_repo", "intent carries no repo_id payload and no repo scope")
		return succeeded(intent), nil
	}
	if h.Loader == nil {
		h.log(ctx, intent, "loader_unavailable", "no candidate loader wired")
		return succeeded(intent), nil
	}
	page, err := h.Loader.LoadCandidates(ctx, repoID)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load drift candidates for repo %q: %w", repoID, err)
	}
	write := DriftedWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		RepoID:       repoID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Suppressions: map[string]int{},
	}
	write.Suppressions[querycodedivergence.RuleBelowFloor] = page.Stats.BelowFloor
	write.Suppressions[FilterNoShingles] = page.Stats.NoShingles
	write.Suppressions[FilterEqualityDuplicate] = page.Stats.EqualityDuplicates
	for _, pair := range page.Pairs {
		admitted, suppressed, reason := ApplyRules(pair)
		if suppressed {
			write.Suppressions[reason]++
			continue
		}
		write.Pairs = append(write.Pairs, admitted)
	}
	write.BudgetExhausted = page.Stats.BudgetExhausted
	if h.Writer == nil {
		h.log(ctx, intent, "writer_unavailable", fmt.Sprintf(
			"evaluated %d pairs, %d admitted, durable write skipped", len(page.Pairs), len(write.Pairs)))
		return succeeded(intent), nil
	}
	if _, err := h.Writer.WriteDriftedFindings(ctx, write); err != nil {
		return reducercontract.Result{}, fmt.Errorf("write drifted findings for repo %q: %w", repoID, err)
	}
	return succeeded(intent), nil
}

// driftedRepoID resolves the repo a drift intent targets: the repo_id
// intent payload first (authoritative, stamped by the projector trigger),
// else the repo: scope prefix. Empty means the intent is structurally
// unusable for this domain.
func driftedRepoID(intent reducercontract.Intent) string {
	if repoID, _ := intent.Payload["repo_id"].(string); strings.TrimSpace(repoID) != "" {
		return strings.TrimSpace(repoID)
	}
	if rest, ok := strings.CutPrefix(strings.TrimSpace(intent.ScopeID), repoScopePrefix); ok && strings.TrimSpace(rest) != "" {
		return strings.TrimSpace(rest)
	}
	return ""
}

func succeeded(intent reducercontract.Intent) reducercontract.Result {
	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   reducercontract.ResultStatusSucceeded,
	}
}

func (h CodeDriftedHandler) log(ctx context.Context, intent reducercontract.Intent, failureClass, reason string) {
	if h.Logger == nil {
		return
	}
	h.Logger.InfoContext(ctx, "code drifted rejection",
		"intent_id", intent.IntentID,
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		telemetry.LogKeyFailureClass, failureClass,
		"reason", reason,
	)
}
