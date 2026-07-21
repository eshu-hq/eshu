// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// DriftEvidenceLoader supplies the joined per-address rows the drift handler
// classifies. The reducer is intentionally agnostic about where the rows come
// from: a real implementation queries the canonical TerraformResource +
// TerraformStateResource projections; the test implementation returns a
// hand-built slice. The loader is the seam that hides the cross-scope join
// from the handler.
type DriftEvidenceLoader interface {
	// LoadDriftEvidence returns the joined drift rows for one state-snapshot
	// scope and the supplied config-side commit anchor. The returned slice
	// MUST contain at most one row per Terraform resource address; addresses
	// without disagreement may be omitted.
	LoadDriftEvidence(
		ctx context.Context,
		stateScopeID string,
		anchor tfstatebackend.CommitAnchor,
	) ([]tfconfigstate.AddressedRow, error)
}

// DriftRejection captures a structured-log payload for non-fatal drift
// rejections (ambiguous backend owner, missing prior generation, etc.). The
// handler stamps the rejection's failure_class on the structured log via
// telemetry.LogKeyFailureClass and returns Status=Succeeded — operator-
// actionable rejections must not become terminal failures.
type DriftRejection struct {
	FailureClass string
	Reason       string
}

// TerraformConfigStateDriftHandler reconciles Terraform config facts (parsed
// HCL) against Terraform state facts to emit drift candidates. The handler
// joins the two scopes via the tfstatebackend resolver, builds one Candidate
// per drifted address (carrying cross-scope EvidenceAtoms), and hands the
// candidate slice to the correlation engine to record the deterministic
// explain trace.
type TerraformConfigStateDriftHandler struct {
	// Resolver picks the latest sealed config snapshot owning the state
	// snapshot's backend. May be nil in fresh-bootstrap scenarios; the
	// handler then treats every intent as "no owner" and returns success
	// without emitting drift counters.
	Resolver *tfstatebackend.Resolver
	// EvidenceLoader returns the joined per-address rows for one state
	// scope. May be nil; the handler then returns success without drift
	// (no observable input).
	EvidenceLoader DriftEvidenceLoader
	// Instruments holds the two correlation counters
	// (CorrelationRuleMatches and CorrelationDriftDetected). May be nil;
	// the handler then skips telemetry but still classifies for the
	// structured log.
	Instruments *telemetry.Instruments
	// Logger receives the structured logs the handler emits for every
	// drift candidate and every operator-actionable rejection. May be nil;
	// the handler then drops logs.
	Logger *slog.Logger
	// Writer persists admitted per-address findings and ambiguous-owner
	// rejections as durable reducer facts (issue #5442). May be nil; the
	// handler then keeps today's counter+log-only behavior and does not
	// publish a durable read model. Counters and logs remain a parallel
	// signal to the durable write, not a replacement for it, when Writer is
	// set.
	Writer TerraformConfigStateDriftFindingWriter
}

// Handle executes the drift pipeline for one reducer intent. The handler:
//
//  1. Rejects intents that target a different domain.
//  2. Resolves the config-side commit anchor for the state snapshot.
//  3. Loads joined drift rows and builds correlation candidates.
//  4. Runs engine.Evaluate against the rule pack.
//  5. Emits two counters per admitted candidate:
//     - eshu_dp_correlation_rule_matches_total{pack, rule}
//     - eshu_dp_correlation_drift_detected_total{pack, rule, drift_kind}
//
// Non-fatal rejections (no owner, ambiguous owner, no drift rows) return
// Result{Status: ResultStatusSucceeded} and emit a structured log only;
// they are operator-actionable, not runtime failures.
func (h TerraformConfigStateDriftHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainConfigStateDrift {
		return Result{}, fmt.Errorf(
			"terraform_config_state_drift handler does not accept domain %q",
			intent.Domain,
		)
	}

	scopeID := intent.ScopeID
	backendKind, locatorHash, err := parseDriftIntentScope(intent)
	if err != nil {
		// Structural mismatch on the intent shape itself — operator-actionable.
		h.logRejection(ctx, intent, DriftRejection{
			FailureClass: "scope_not_state_snapshot",
			Reason:       err.Error(),
		})
		return Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		}, nil
	}

	if h.Resolver == nil {
		h.logRejection(ctx, intent, DriftRejection{
			FailureClass: "resolver_unavailable",
			Reason:       "no tfstatebackend resolver wired",
		})
		return Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		}, nil
	}

	anchor, resolveErr := h.Resolver.ResolveConfigCommitForBackend(ctx, backendKind, locatorHash)
	if errors.Is(resolveErr, tfstatebackend.ErrNoConfigRepoOwnsBackend) {
		h.logRejection(ctx, intent, DriftRejection{
			FailureClass: "no_config_repo_owns_backend",
			Reason:       resolveErr.Error(),
		})
		return Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		}, nil
	}
	if errors.Is(resolveErr, tfstatebackend.ErrAmbiguousBackendOwner) {
		h.logRejection(ctx, intent, DriftRejection{
			FailureClass: "ambiguous_backend_owner",
			Reason:       resolveErr.Error(),
		})
		h.writeAmbiguousOwner(ctx, intent, backendKind, locatorHash, resolveErr)
		return Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		}, nil
	}
	if resolveErr != nil {
		return Result{}, fmt.Errorf("resolve config commit: %w", resolveErr)
	}

	if h.EvidenceLoader == nil {
		h.logRejection(ctx, intent, DriftRejection{
			FailureClass: "evidence_loader_unavailable",
			Reason:       "no drift evidence loader wired",
		})
		return Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		}, nil
	}

	rows, err := h.EvidenceLoader.LoadDriftEvidence(ctx, scopeID, anchor)
	if err != nil {
		return Result{}, fmt.Errorf("load drift evidence: %w", err)
	}

	candidates := tfconfigstate.BuildCandidates(rows, anchor, scopeID)
	pack := rules.TerraformConfigStateDriftRulePack()
	evaluation, err := engine.Evaluate(pack, candidates)
	if err != nil {
		return Result{}, fmt.Errorf("evaluate drift rule pack: %w", err)
	}

	admitted := h.emitTelemetry(ctx, intent, pack, evaluation)

	canonicalWrites := 0
	if h.Writer != nil {
		admittedCandidates := admittedDriftCandidates(evaluation)
		if len(admittedCandidates) > 0 {
			writeResult, writeErr := h.Writer.WriteTerraformConfigStateDriftFindings(ctx, TerraformConfigStateDriftWrite{
				IntentID:     intent.IntentID,
				ScopeID:      intent.ScopeID,
				GenerationID: intent.GenerationID,
				SourceSystem: intent.SourceSystem,
				Cause:        intent.Cause,
				BackendKind:  backendKind,
				LocatorHash:  locatorHash,
				Candidates:   admittedCandidates,
			})
			if writeErr != nil {
				return Result{}, fmt.Errorf("write terraform config state drift findings: %w", writeErr)
			}
			canonicalWrites = writeResult.CanonicalWrites
		}
	}

	return Result{
		IntentID:        intent.IntentID,
		Domain:          intent.Domain,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("drift candidates admitted: %d", admitted),
		CanonicalWrites: canonicalWrites,
	}, nil
}

// admittedDriftCandidates filters the engine evaluation down to admitted
// candidates in evaluation order (CorrelationKey-sorted; see engine.Evaluate),
// giving the writer a deterministic input order.
func admittedDriftCandidates(evaluation engine.Evaluation) []model.Candidate {
	out := make([]model.Candidate, 0, len(evaluation.Results))
	for _, result := range evaluation.Results {
		if result.Candidate.State == model.CandidateStateAdmitted {
			out = append(out, result.Candidate)
		}
	}
	return out
}

// writeAmbiguousOwner persists one durable "ambiguous" finding for the whole
// state-snapshot scope when backend-owner resolution finds more than one
// candidate config repo. No-op when Writer is nil (counters/logs-only mode)
// or when resolveErr does not carry the candidate rows (should not happen
// given the errors.Is(resolveErr, tfstatebackend.ErrAmbiguousBackendOwner)
// guard at the call site, but the handler must not panic on a future
// tfstatebackend change that stops wrapping the sentinel with candidates).
// Write failures are logged, not returned as a Handle() error: the ambiguous
// case is already a non-fatal, operator-actionable rejection per
// DriftRejection's contract (Result{Status: Succeeded}), and failing the
// whole intent over a best-effort durability write would turn an
// operator-actionable warning into a retry storm.
func (h TerraformConfigStateDriftHandler) writeAmbiguousOwner(
	ctx context.Context,
	intent Intent,
	backendKind string,
	locatorHash string,
	resolveErr error,
) {
	if h.Writer == nil {
		return
	}
	var ambiguous *tfstatebackend.AmbiguousBackendOwnerError
	if !errors.As(resolveErr, &ambiguous) || len(ambiguous.Candidates) == 0 {
		return
	}
	_, writeErr := h.Writer.WriteTerraformConfigStateDriftFindings(ctx, TerraformConfigStateDriftWrite{
		IntentID:        intent.IntentID,
		ScopeID:         intent.ScopeID,
		GenerationID:    intent.GenerationID,
		SourceSystem:    intent.SourceSystem,
		Cause:           intent.Cause,
		BackendKind:     backendKind,
		LocatorHash:     locatorHash,
		AmbiguousOwners: ambiguous.Candidates,
	})
	if writeErr != nil && h.Logger != nil {
		h.Logger.LogAttrs(
			ctx, slog.LevelWarn, "drift ambiguous owner durable write failed",
			log.Domain(string(intent.Domain)),
			log.ScopeID(intent.ScopeID),
			log.GenerationID(intent.GenerationID),
			slog.String("write.error", writeErr.Error()),
		)
	}
}

// driftIntentScopePrefix is the canonical state_snapshot scope prefix per
// go/internal/scope/tfstate.go:33-40. The scope shape is
// state_snapshot:<backend_kind>:<locator_hash> where locator_hash is a
// hex-safe value with no embedded colons.
const driftIntentScopePrefix = "state_snapshot:"

// parseDriftIntentScope verifies the intent's ScopeID is a state_snapshot
// scope and pulls (backend_kind, locator_hash) out of it. Locator hashes are
// hex-safe by construction (`go/internal/scope/tfstate.go`); a colon inside
// the locator hash field indicates either a malformed scope or a non-canonical
// emitter and is rejected explicitly.
func parseDriftIntentScope(intent Intent) (backendKind, locatorHash string, err error) {
	rest, ok := strings.CutPrefix(intent.ScopeID, driftIntentScopePrefix)
	if !ok {
		return "", "", fmt.Errorf("scope %q is not a state_snapshot scope", intent.ScopeID)
	}
	backendKind, locatorHash, ok = strings.Cut(rest, ":")
	if !ok || strings.TrimSpace(backendKind) == "" || strings.TrimSpace(locatorHash) == "" {
		return "", "", fmt.Errorf("scope %q must be state_snapshot:<backend_kind>:<locator_hash>", intent.ScopeID)
	}
	if strings.Contains(locatorHash, ":") {
		return "", "", fmt.Errorf("scope %q locator hash contains forbidden colon", intent.ScopeID)
	}
	return backendKind, locatorHash, nil
}

// emitTelemetry walks the engine evaluation, increments the two correlation
// counters per admitted candidate, and emits a structured log carrying the
// resource address (high-cardinality attribute, kept out of metric labels).
// Returns the number of admitted candidates.
func (h TerraformConfigStateDriftHandler) emitTelemetry(
	ctx context.Context,
	intent Intent,
	pack rules.RulePack,
	evaluation engine.Evaluation,
) int {
	admitted := 0
	for _, result := range evaluation.Results {
		if result.Candidate.State != model.CandidateStateAdmitted {
			continue
		}
		admitted++

		driftKind := readDriftKindAtom(result.Candidate)
		address := result.Candidate.CorrelationKey

		// Emit rule-match counter increments using the engine's
		// Result.MatchCounts. The engine populates this map for RuleKindMatch
		// rules only (correlation/engine/engine.go:50-56), keyed by rule name
		// with boundedMatchCount(MaxMatches, len(Evidence)). Iteration order
		// is sorted by rule name for deterministic test capture; counter
		// addition is commutative, so order does not affect end values.
		if h.Instruments != nil && h.Instruments.CorrelationRuleMatches != nil {
			matchRuleNames := make([]string, 0, len(result.MatchCounts))
			for ruleName := range result.MatchCounts {
				matchRuleNames = append(matchRuleNames, ruleName)
			}
			sort.Strings(matchRuleNames)
			for _, ruleName := range matchRuleNames {
				count := result.MatchCounts[ruleName]
				if count <= 0 {
					continue
				}
				h.Instruments.CorrelationRuleMatches.Add(ctx, int64(count), metric.WithAttributes(
					attribute.String(telemetry.MetricDimensionPack, pack.Name),
					attribute.String(telemetry.MetricDimensionRule, ruleName),
				))
			}
		}

		// Emit one drift_detected increment per admitted candidate with
		// drift_kind label. Resource address goes into the structured log,
		// not into the metric label space.
		if h.Instruments != nil && h.Instruments.CorrelationDriftDetected != nil && driftKind != "" {
			h.Instruments.CorrelationDriftDetected.Add(ctx, 1, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionPack, pack.Name),
				attribute.String(telemetry.MetricDimensionRule, rules.TerraformConfigStateDriftRuleAdmitDriftEvidence),
				attribute.String(telemetry.MetricDimensionDriftKind, driftKind),
			))
		}

		if h.Logger != nil {
			h.Logger.LogAttrs(
				ctx, slog.LevelInfo, "drift candidate admitted",
				log.Domain(string(intent.Domain)),
				log.ScopeID(intent.ScopeID),
				log.GenerationID(intent.GenerationID),
				slog.String("drift.pack", pack.Name),
				slog.String("drift.kind", driftKind),
				slog.String("drift.address", address),
			)
		}
	}
	return admitted
}

func readDriftKindAtom(candidate model.Candidate) string {
	for _, atom := range candidate.Evidence {
		if atom.EvidenceType == tfconfigstate.EvidenceTypeDriftKind {
			return atom.Value
		}
	}
	return ""
}

// logRejection emits the operator-actionable warning for non-fatal drift
// rejections. The request ctx is threaded through so the rejection is
// attributable to the reducer run that produced it (trace/span correlation,
// cancellation, structured-log handlers that read ctx values). Passing
// context.Background() here would orphan the log from any active span.
func (h TerraformConfigStateDriftHandler) logRejection(ctx context.Context, intent Intent, rejection DriftRejection) {
	if h.Logger == nil {
		return
	}
	h.Logger.LogAttrs(
		ctx, slog.LevelWarn, "drift candidate rejected",
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.FailureClass(rejection.FailureClass),
		slog.String("rejection.reason", rejection.Reason),
	)
}
