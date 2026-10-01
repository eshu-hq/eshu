// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"strings"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// CandidateSelectionReason is the closed taxonomy for a zero-or-more
// selection outcome (#7384). A bare selected count misleads because
// foreign-key intents legitimately select zero, so the reason is mandatory:
// no_keys means the intent carried no entity keys; no_admitted_candidates
// means the scope admitted nothing to select from (legitimate empty);
// key_match means at least one candidate selected; no_key_match means keys
// and admitted candidates exist but select nothing (the mismatch signal);
// foreign_key_expected means the keys reference repositories outside this
// scope, which is legitimate for cross-repo references (refined by callers
// that hold the scope repository set; the filter itself reports
// no_key_match).
type CandidateSelectionReason string

const (
	SelectionNoKeys               CandidateSelectionReason = "no_keys"
	SelectionNoAdmittedCandidates CandidateSelectionReason = "no_admitted_candidates"
	SelectionKeyMatch             CandidateSelectionReason = "key_match"
	SelectionNoKeyMatch           CandidateSelectionReason = "no_key_match"
	SelectionForeignKeyExpected   CandidateSelectionReason = "foreign_key_expected"
)

// CandidateSelectionReport splits a selection outcome into admitted vs
// selected counts plus the reason, so a zero selection is distinguishable
// from a mismatch. It is pure data (no timestamps, no logging) so Ifá's
// direct-call contract on the extract seam keeps holding.
type CandidateSelectionReport struct {
	Admitted int
	Selected int
	Reason   CandidateSelectionReason
}

func filterDeployableUnitCandidates(
	candidates []WorkloadCandidate,
	entityKeys map[string]struct{},
) ([]WorkloadCandidate, CandidateSelectionReport) {
	report := CandidateSelectionReport{Admitted: len(candidates)}
	if len(entityKeys) == 0 {
		report.Reason = SelectionNoKeys
		return nil, report
	}
	if len(candidates) == 0 {
		report.Reason = SelectionNoAdmittedCandidates
		return nil, report
	}
	filtered := make([]WorkloadCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		for _, key := range candidateIdentityKeys(candidate) {
			if _, ok := entityKeys[strings.ToLower(strings.TrimSpace(key))]; ok {
				filtered = append(filtered, candidate)
				break
			}
		}
	}
	report.Selected = len(filtered)
	if len(filtered) > 0 {
		report.Reason = SelectionKeyMatch
	} else {
		report.Reason = SelectionNoKeyMatch
	}
	return filtered, report
}

// refineCandidateSelectionReason upgrades a no_key_match report to
// foreign_key_expected when none of the intent's key identities intersect
// the scope's repository IDs: the keys reference repositories outside this
// scope, which is legitimate for cross-repo references and must not read as
// a selection mismatch. Reports with any other reason pass through
// unchanged. It is pure data comparison (no I/O, no clock) so logging
// layers can apply it without breaking Ifá's direct-call contract.
func refineCandidateSelectionReason(
	report CandidateSelectionReport,
	intentKeys []string,
	scopeRepoIDs []string,
) CandidateSelectionReason {
	if report.Reason != SelectionNoKeyMatch {
		return report.Reason
	}
	scope := make(map[string]struct{}, len(scopeRepoIDs))
	for _, id := range scopeRepoIDs {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			scope[id] = struct{}{}
			if alias := normalizedEntityKey(id); alias != "" {
				scope[alias] = struct{}{}
			}
		}
	}
	for _, key := range intentKeys {
		if key = strings.ToLower(strings.TrimSpace(key)); key != "" {
			if _, ok := scope[key]; ok {
				return SelectionNoKeyMatch
			}
			if alias := normalizedEntityKey(key); alias != "" {
				if _, ok := scope[alias]; ok {
					return SelectionNoKeyMatch
				}
			}
		}
	}
	return SelectionForeignKeyExpected
}

// logDeployableUnitCorrelationCompleted emits the single structured
// completion log for one deployable-unit correlation pass, carrying the
// candidate selection counts and reason (Q4) so an operator can distinguish
// a foreign-key zero from a mismatch without reading keys — mirroring
// logWorkloadMaterializationCompleted on the workload-materialization path.
// A nil logger falls back to slog.Default, kept for test wiring only.
func logDeployableUnitCorrelationCompleted(
	ctx context.Context,
	logger *slog.Logger,
	intent Intent,
	selection CandidateSelectionReport,
	edgeRowCount int,
) {
	lg := logger
	if lg == nil {
		lg = slog.Default()
	}
	lg.InfoContext(
		ctx, "deployable unit correlation completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(DomainDeployableUnitCorrelation)),
		slog.String("intent_id", intent.IntentID),
		slog.Any("entity_keys", intent.EntityKeys),
		slog.Int("scope_candidate_count", selection.Admitted),
		slog.Int("selected_candidate_count", selection.Selected),
		slog.String("selection_reason", string(selection.Reason)),
		slog.Int("edge_row_count", edgeRowCount),
	)
}
