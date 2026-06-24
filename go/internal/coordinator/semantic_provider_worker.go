// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

// Semantic-provider execution outcomes are bounded, low-cardinality reason codes
// safe to attach to telemetry, logs, and governance audit labels. They never
// carry a provider host, endpoint, URL, credential, or raw prompt/response.
const (
	// SemanticProviderOutcomeEgressDenied marks a claim skipped fail-closed because
	// the claim-path egress re-check denied or found no allow rule.
	SemanticProviderOutcomeEgressDenied = "egress_denied"
	// SemanticProviderOutcomeEgressPolicyMissing marks a claim skipped fail-closed
	// because no semantic provider egress policy was configured.
	SemanticProviderOutcomeEgressPolicyMissing = "egress_policy_missing"
	// SemanticProviderOutcomeProviderDisabled marks a claim that passed the egress
	// gate but was terminated because no real provider client is enabled. This is
	// the default no-network outcome.
	SemanticProviderOutcomeProviderDisabled = "provider_disabled"
	// SemanticProviderOutcomeDispatched marks a claim that passed the egress gate
	// and was dispatched through an explicitly enabled provider client.
	SemanticProviderOutcomeDispatched = "dispatched"
)

// providerDisabledFailureClass labels the terminal failure recorded when the
// default no-network client refuses dispatch.
const providerDisabledFailureClass = "provider_execution_not_enabled"

// SemanticDispatchRequest is the audit-safe input a provider client receives.
// It carries only redacted identity and posture, never raw prompt content,
// provider host, endpoint, URL, or credential. The default client ignores it.
type SemanticDispatchRequest struct {
	JobID                string
	WorkItemID           string
	ScopeID              string
	GenerationID         string
	SourceClass          string
	ProviderKind         string
	ProviderProfileID    string
	ProviderProfileClass string
}

// SemanticDispatchResult is the audit-safe outcome a provider client returns.
type SemanticDispatchResult struct {
	// ResponseHash is a non-secret content hash recorded on success. It must never
	// contain a raw provider response body.
	ResponseHash string
}

// SemanticProviderClient dispatches a redacted, egress-approved semantic
// extraction job to an outbound provider.
//
// SAFETY CONTRACT: the worker only ever calls Dispatch AFTER the claim-path
// egress gate allows the claimed provider profile and source class. The default
// implementation (DisabledSemanticProviderClient) performs no network I/O and
// returns ErrSemanticProviderExecutionNotEnabled. A concrete network client that
// makes real outbound LLM/provider calls is intentionally out of scope here and
// must be supplied by a future PR after security and schema review, gated behind
// the default-OFF ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED flag.
type SemanticProviderClient interface {
	// Enabled reports whether the client may perform real outbound provider work.
	// The worker treats a disabled client as a terminal no-network outcome and
	// never calls Dispatch on it.
	Enabled() bool
	// Dispatch sends one egress-approved job to the provider. Implementations MUST
	// NOT be called by the worker unless Enabled returns true.
	Dispatch(context.Context, SemanticDispatchRequest) (SemanticDispatchResult, error)
}

// ErrSemanticProviderExecutionNotEnabled is the terminal error the default
// no-network client returns to prove provider traffic is disabled by default.
var ErrSemanticProviderExecutionNotEnabled = errors.New("semantic provider execution is not enabled")

// DisabledSemanticProviderClient is the default semantic provider client. It is
// the no-network safety default: Enabled always reports false and Dispatch never
// performs outbound I/O. With this client the worker claims, evaluates egress,
// audits decisions, and terminates allowed jobs with a redacted
// provider-disabled outcome without ever contacting a provider.
type DisabledSemanticProviderClient struct{}

// Enabled reports false: the default client never permits provider traffic.
func (DisabledSemanticProviderClient) Enabled() bool { return false }

// Dispatch never performs network I/O and always returns the terminal
// not-enabled error.
func (DisabledSemanticProviderClient) Dispatch(
	context.Context,
	SemanticDispatchRequest,
) (SemanticDispatchResult, error) {
	return SemanticDispatchResult{}, ErrSemanticProviderExecutionNotEnabled
}

// SemanticExtractionClaimer is the narrow durable claim surface the
// semantic-provider execution worker needs.
type SemanticExtractionClaimer interface {
	ClaimNext(
		ctx context.Context,
		scopeID string,
		leaseOwner string,
		now time.Time,
		leaseFor time.Duration,
	) (semanticqueue.Record, bool, error)
	SkipClaimByPolicy(
		ctx context.Context,
		record semanticqueue.Record,
		leaseOwner string,
		now time.Time,
		reasonCode string,
	) error
	SucceedClaim(
		ctx context.Context,
		record semanticqueue.Record,
		leaseOwner string,
		now time.Time,
		responseHash string,
		budget semanticqueue.BudgetDecision,
	) error
	DeadLetterClaim(
		ctx context.Context,
		record semanticqueue.Record,
		leaseOwner string,
		now time.Time,
		failure semanticqueue.Failure,
	) error
}

// SemanticProviderWorkerConfig configures the semantic-provider execution worker.
// The worker is OFF by default and ships no real provider traffic: it dispatches
// only when both ExecutionEnabled is true AND the supplied Client reports
// Enabled. The egress gate runs before any dispatch regardless of these flags.
type SemanticProviderWorkerConfig struct {
	// Enabled turns the claim loop on. Default false.
	Enabled bool
	// ExecutionEnabled is the explicit, documented, default-OFF flag that, together
	// with an Enabled client, permits real outbound provider traffic. Default false.
	ExecutionEnabled bool
	// LeaseOwner identifies this worker for lease fencing.
	LeaseOwner string
	// LeaseTTL bounds how long a claim is held.
	LeaseTTL time.Duration
	// MaxClaimsPerPass bounds how many jobs one pass drains, preventing a single
	// scope from starving the loop.
	MaxClaimsPerPass int
	// ScopeIDs are the queue scopes the worker drains.
	ScopeIDs []string
	// Policy is the semantic extraction policy re-checked at claim time.
	Policy semanticpolicy.Policy
}

// SemanticProviderWorker is the egress-gated semantic-provider execution worker.
// It claims semantic extraction jobs, re-checks egress fail-closed before any
// provider dispatch, emits redacted governance audit and telemetry for every
// decision, and dispatches only through an explicitly enabled provider client.
type SemanticProviderWorker struct {
	Config          SemanticProviderWorkerConfig
	Claimer         SemanticExtractionClaimer
	Client          SemanticProviderClient
	GovernanceAudit GovernanceAuditAppender
	Metrics         SemanticProviderWorkerMetrics
	Logger          loggerInterface
	Clock           func() time.Time
}

// loggerInterface is the minimal structured-logging surface the worker uses so
// callers may pass *slog.Logger or nil.
type loggerInterface interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}

func (w SemanticProviderWorker) now() time.Time {
	if w.Clock != nil {
		return w.Clock()
	}
	return time.Now()
}

func (w SemanticProviderWorker) client() SemanticProviderClient {
	if w.Client != nil {
		return w.Client
	}
	return DisabledSemanticProviderClient{}
}

// Run drains the configured scopes once, claiming and gating each job. It is
// safe to call repeatedly on a ticker. Run is a no-op when the worker is
// disabled or no claimer is configured.
func (w SemanticProviderWorker) Run(ctx context.Context) error {
	if !w.Config.Enabled || w.Claimer == nil {
		return nil
	}
	leaseTTL := w.Config.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	maxClaims := w.Config.MaxClaimsPerPass
	if maxClaims <= 0 {
		maxClaims = 32
	}
	for _, scopeID := range w.Config.ScopeIDs {
		for claimed := 0; claimed < maxClaims; claimed++ {
			if err := ctx.Err(); err != nil {
				return nil
			}
			record, ok, err := w.Claimer.ClaimNext(ctx, scopeID, w.Config.LeaseOwner, w.now().UTC(), leaseTTL)
			if err != nil {
				return fmt.Errorf("claim semantic provider job for scope %q: %w", scopeID, err)
			}
			if !ok {
				break
			}
			if err := w.handleClaim(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

// handleClaim runs the fail-closed egress gate and, only if allowed, the
// enabled-client dispatch path. The egress gate ALWAYS runs before any provider
// dispatch is even considered.
func (w SemanticProviderWorker) handleClaim(ctx context.Context, record semanticqueue.Record) error {
	now := w.now().UTC()
	decision := semanticpolicy.EvaluateEgress(w.Config.Policy, record.ProviderProfileID, record.SourceClass)
	if !decision.Allowed {
		return w.skipDenied(ctx, record, now, decision)
	}

	// Egress allowed. Dispatch is permitted ONLY when both the execution flag is
	// set AND the client reports enabled. The default client is disabled, so this
	// terminates as a redacted no-network outcome.
	cli := w.client()
	if !w.Config.ExecutionEnabled || !cli.Enabled() {
		return w.terminateProviderDisabled(ctx, record, now)
	}
	return w.dispatch(ctx, cli, record, now)
}

func (w SemanticProviderWorker) skipDenied(
	ctx context.Context,
	record semanticqueue.Record,
	now time.Time,
	decision semanticpolicy.EgressDecision,
) error {
	outcome := SemanticProviderOutcomeEgressDenied
	auditDecision := governanceaudit.DecisionDenied
	if decision.Reason == semanticpolicy.ReasonEgressPolicyMissing {
		outcome = SemanticProviderOutcomeEgressPolicyMissing
		auditDecision = governanceaudit.DecisionUnavailable
	}
	if err := w.recordEgressAudit(ctx, record, now, auditDecision, decision.Reason); err != nil {
		return err
	}
	if err := w.Claimer.SkipClaimByPolicy(ctx, record, w.Config.LeaseOwner, now, decision.Reason); err != nil {
		return fmt.Errorf("skip denied semantic provider job: %w", err)
	}
	w.recordClaim(ctx, record, outcome)
	if w.Logger != nil {
		w.Logger.Warn(
			"semantic provider worker skipped job by egress policy",
			"provider_kind", record.ProviderKind,
			"provider_profile_class", record.ProviderProfileClass,
			"source_class", record.SourceClass,
			"reason", decision.Reason,
		)
	}
	return nil
}

func (w SemanticProviderWorker) terminateProviderDisabled(
	ctx context.Context,
	record semanticqueue.Record,
	now time.Time,
) error {
	failure := semanticqueue.Failure{Class: providerDisabledFailureClass}
	if err := w.Claimer.DeadLetterClaim(ctx, record, w.Config.LeaseOwner, now, failure); err != nil {
		return fmt.Errorf("terminate provider-disabled semantic job: %w", err)
	}
	w.recordClaim(ctx, record, SemanticProviderOutcomeProviderDisabled)
	if w.Logger != nil {
		w.Logger.Info(
			"semantic provider worker terminated egress-allowed job: provider execution disabled",
			"provider_kind", record.ProviderKind,
			"provider_profile_class", record.ProviderProfileClass,
			"source_class", record.SourceClass,
			"reason", providerDisabledFailureClass,
		)
	}
	return nil
}

func (w SemanticProviderWorker) dispatch(
	ctx context.Context,
	cli SemanticProviderClient,
	record semanticqueue.Record,
	now time.Time,
) error {
	result, err := cli.Dispatch(ctx, SemanticDispatchRequest{
		JobID:                record.JobID,
		WorkItemID:           record.WorkItemID,
		ScopeID:              record.ScopeID,
		GenerationID:         record.GenerationID,
		SourceClass:          record.SourceClass,
		ProviderKind:         record.ProviderKind,
		ProviderProfileID:    record.ProviderProfileID,
		ProviderProfileClass: record.ProviderProfileClass,
	})
	if err != nil {
		failure := semanticqueue.Failure{Class: semanticqueue.FailureClassProviderUnavailable}
		if dlErr := w.Claimer.DeadLetterClaim(ctx, record, w.Config.LeaseOwner, now, failure); dlErr != nil {
			return fmt.Errorf("dead-letter failed semantic dispatch: %w", dlErr)
		}
		w.recordClaim(ctx, record, semanticqueue.FailureClassProviderUnavailable)
		return nil
	}
	if err := w.Claimer.SucceedClaim(
		ctx, record, w.Config.LeaseOwner, now, result.ResponseHash, record.Budget,
	); err != nil {
		return fmt.Errorf("persist semantic dispatch success: %w", err)
	}
	w.recordClaim(ctx, record, SemanticProviderOutcomeDispatched)
	return nil
}

func (w SemanticProviderWorker) recordEgressAudit(
	ctx context.Context,
	record semanticqueue.Record,
	now time.Time,
	decision governanceaudit.Decision,
	reasonCode string,
) error {
	if w.GovernanceAudit == nil {
		return nil
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeSemanticPolicyDecision,
		ActorClass:         governanceaudit.ActorClassServicePrincipal,
		ServicePrincipalID: governanceAuditServiceID,
		ScopeClass:         governanceaudit.ScopeClassProviderProfile,
		ScopeIDHash:        governanceAuditHash("semantic-provider", record.ProviderProfileID, record.SourceClass),
		Decision:           decision,
		ReasonCode:         reasonCode,
		CorrelationID:      governanceAuditCorrelation("semantic-egress", record.ProviderProfileID, record.SourceClass),
		OccurredAt:         now,
	}
	auditCtx, cancel := context.WithTimeout(ctx, governanceAuditAppendTimeout)
	defer cancel()
	if err := w.GovernanceAudit.Append(auditCtx, []governanceaudit.Event{event}); err != nil {
		return fmt.Errorf("append semantic egress audit event: %w", err)
	}
	return nil
}

func (w SemanticProviderWorker) recordClaim(ctx context.Context, record semanticqueue.Record, outcome string) {
	if w.Metrics == nil {
		return
	}
	w.Metrics.RecordSemanticProviderClaim(ctx, SemanticProviderClaimObservation{
		Outcome:              outcome,
		ProviderKind:         record.ProviderKind,
		ProviderProfileClass: record.ProviderProfileClass,
		SourceClass:          record.SourceClass,
	})
}
