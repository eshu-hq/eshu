package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
}

// errHandlerNotImplemented is retained for backward compatibility; it is no
// longer returned by Handle. New callers that want a "no-op" handler should
// wire a TerraformConfigStateDriftHandler with nil EvidenceLoader.
var errHandlerNotImplemented = errors.New("terraform_config_state_drift handler not implemented")

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
		h.logRejection(intent, DriftRejection{
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
		h.logRejection(intent, DriftRejection{
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
		h.logRejection(intent, DriftRejection{
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
		h.logRejection(intent, DriftRejection{
			FailureClass: "ambiguous_backend_owner",
			Reason:       resolveErr.Error(),
		})
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
		h.logRejection(intent, DriftRejection{
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

	return Result{
		IntentID:        intent.IntentID,
		Domain:          intent.Domain,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("drift candidates admitted: %d", admitted),
	}, nil
}

// parseDriftIntentScope verifies the intent's ScopeID is a state_snapshot
// scope and pulls (backend_kind, locator_hash) out of it. The state scope
// convention is "state_snapshot:<backendKind>:<locatorHash>" per
// go/internal/scope/tfstate.go:33-40.
func parseDriftIntentScope(intent Intent) (backendKind, locatorHash string, err error) {
	const prefix = "state_snapshot:"
	scope := intent.ScopeID
	if len(scope) <= len(prefix) || scope[:len(prefix)] != prefix {
		return "", "", fmt.Errorf("scope %q is not a state_snapshot scope", scope)
	}
	rest := scope[len(prefix):]
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' {
			backendKind = rest[:i]
			locatorHash = rest[i+1:]
			break
		}
	}
	if backendKind == "" || locatorHash == "" {
		return "", "", fmt.Errorf("scope %q missing backend_kind or locator_hash", scope)
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

		// Emit one rule-match increment per rule in the pack so the explain
		// trace's MatchCounts surfaces through the counter dimension.
		if h.Instruments != nil && h.Instruments.CorrelationRuleMatches != nil {
			for _, rule := range pack.Rules {
				h.Instruments.CorrelationRuleMatches.Add(ctx, 1, metric.WithAttributes(
					attribute.String(telemetry.MetricDimensionPack, pack.Name),
					attribute.String(telemetry.MetricDimensionRule, rule.Name),
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
			h.Logger.LogAttrs(ctx, slog.LevelInfo, "drift candidate admitted",
				slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
				slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
				slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
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

func (h TerraformConfigStateDriftHandler) logRejection(intent Intent, rejection DriftRejection) {
	if h.Logger == nil {
		return
	}
	h.Logger.LogAttrs(context.Background(), slog.LevelWarn, "drift candidate rejected",
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyFailureClass, rejection.FailureClass),
		slog.String("rejection.reason", rejection.Reason),
	)
}
