// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimControlStore is the workflow claim surface needed by scanner workers.
type ClaimControlStore interface {
	ClaimNextEligible(context.Context, workflow.ClaimSelector, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, bool, error)
	HeartbeatClaim(context.Context, workflow.ClaimMutation) error
	CompleteClaim(context.Context, workflow.ClaimMutation) error
	FailClaimRetryable(context.Context, workflow.ClaimMutation) error
	FailClaimTerminal(context.Context, workflow.ClaimMutation) error
}

// ClaimedFactCommitter commits scanner-worker source facts under a claim fence.
type ClaimedFactCommitter interface {
	CommitClaimedScopeGeneration(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error
}

// Service claims scanner-worker work items, runs one analyzer, commits source
// facts, and records retry or dead-letter payloads.
type Service struct {
	ControlStore        ClaimControlStore
	Committer           ClaimedFactCommitter
	Analyzer            Analyzer
	AnalyzerKind        AnalyzerKind
	CollectorInstanceID string
	OwnerID             string
	ClaimIDFunc         func() string
	PollInterval        time.Duration
	ClaimLeaseTTL       time.Duration
	HeartbeatInterval   time.Duration
	ResourceLimits      ResourceLimits
	Clock               func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
	Logger              *slog.Logger
}

// Run claims and processes scanner-worker work until the context is canceled.
func (s Service) Run(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		claimID := strings.TrimSpace(s.ClaimIDFunc())
		if claimID == "" {
			return fmt.Errorf("claim id is required")
		}
		item, claim, found, err := s.ControlStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       scope.CollectorScannerWorker,
			CollectorInstanceID: s.CollectorInstanceID,
			OwnerID:             s.OwnerID,
			ClaimID:             claimID,
		}, s.now(), s.ClaimLeaseTTL)
		if err != nil {
			return fmt.Errorf("claim next scanner-worker item: %w", err)
		}
		if !found {
			if err := waitForNextScannerPoll(ctx, s.PollInterval); err != nil {
				return nil
			}
			continue
		}
		if err := s.processClaimed(ctx, item, claim); err != nil {
			return err
		}
	}
}

func (s Service) validate() error {
	if s.ControlStore == nil {
		return errors.New("scanner-worker claim control store is required")
	}
	if s.Committer == nil {
		return errors.New("scanner-worker committer is required")
	}
	if s.Analyzer == nil {
		return errors.New("scanner-worker analyzer is required")
	}
	if lane, ok := AnalyzerLane(s.AnalyzerKind); !ok {
		return fmt.Errorf("unknown analyzer %q", s.AnalyzerKind)
	} else if lane != LaneScannerWorker {
		return fmt.Errorf("analyzer %q belongs to %q, not %q", s.AnalyzerKind, lane, LaneScannerWorker)
	}
	if strings.TrimSpace(s.CollectorInstanceID) == "" {
		return errors.New("collector instance id is required")
	}
	if strings.TrimSpace(s.OwnerID) == "" {
		return errors.New("owner id is required")
	}
	if s.ClaimIDFunc == nil {
		return errors.New("claim id function is required")
	}
	if s.PollInterval <= 0 {
		return errors.New("scanner-worker poll interval must be positive")
	}
	if s.ClaimLeaseTTL <= 0 {
		return errors.New("claim lease TTL must be positive")
	}
	if s.HeartbeatInterval <= 0 {
		return errors.New("claim heartbeat interval must be positive")
	}
	if s.HeartbeatInterval >= s.ClaimLeaseTTL {
		return errors.New("claim heartbeat interval must be less than lease TTL")
	}
	_, err := s.resourceLimits()
	return err
}

func (s Service) processClaimed(ctx context.Context, item workflow.WorkItem, claim workflow.Claim) error {
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanScannerWorkerClaimProcess)
		defer span.End()
	}
	limits, err := s.resourceLimits()
	if err != nil {
		return err
	}
	target, err := TargetScopeFromWorkItem(item)
	if err != nil {
		return err
	}
	input, err := NewClaimInputAt(item, claim, s.AnalyzerKind, target, limits, s.now())
	if err != nil {
		return err
	}
	mutation := s.claimMutation(item, claim)
	if err := s.ControlStore.HeartbeatClaim(ctx, mutation); err != nil {
		return fmt.Errorf("heartbeat scanner-worker claim: %w", err)
	}
	s.recordClaimStart(ctx, input, item)

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatErr := s.startHeartbeatLoop(heartbeatCtx, mutation)
	defer stopHeartbeat()

	result, err := s.analyze(ctx, input)
	if err != nil {
		stopHeartbeat()
		if drainErr := drainScannerHeartbeatError(heartbeatErr); drainErr != nil {
			return drainErr
		}
		return s.recordAnalyzerFailure(ctx, mutation, input, err)
	}
	if err := ValidateFactOutput(input, result.Output); err != nil {
		stopHeartbeat()
		if drainErr := drainScannerHeartbeatError(heartbeatErr); drainErr != nil {
			return drainErr
		}
		return s.recordFailure(ctx, mutation, input, FailureDeadLetter, FailureClassAnalyzerFailed, result.Usage)
	}

	commitMutation := mutation
	commitMutation.ObservedAt = s.now()
	scopeValue, generation := scopeAndGenerationForInput(input, s.now())
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanScannerWorkerFactEmitBatch)
		defer span.End()
	}
	if err := s.Committer.CommitClaimedScopeGeneration(
		ctx,
		commitMutation,
		scopeValue,
		generation,
		factChannel(result.Output.Facts),
	); err != nil {
		stopHeartbeat()
		if drainErr := drainScannerHeartbeatError(heartbeatErr); drainErr != nil {
			return drainErr
		}
		s.logCommitFailure(input, err)
		return s.recordFailure(ctx, mutation, input, FailureRetryable, FailureClassCommitFailed, result.Usage)
	}

	stopHeartbeat()
	if err := drainScannerHeartbeatError(heartbeatErr); err != nil {
		return err
	}
	if err := s.ControlStore.CompleteClaim(ctx, mutation); err != nil {
		return fmt.Errorf("complete scanner-worker claim: %w", err)
	}
	s.recordSuccess(ctx, input, result)
	return nil
}

func (s Service) analyze(ctx context.Context, input ClaimInput) (AnalyzerResult, error) {
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanScannerWorkerAnalyze)
		defer span.End()
	}
	analyzeCtx, cancel := context.WithTimeout(ctx, input.Limits.Timeout)
	defer cancel()
	start := s.now()
	result, err := s.Analyzer.Analyze(analyzeCtx, input)
	if errors.Is(analyzeCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		err = NewRetryableAnalyzerFailure(FailureClassTimeout, result.Usage, context.DeadlineExceeded)
	}
	duration := s.now().Sub(start).Seconds()
	if duration < 0 {
		duration = 0
	}
	s.recordScanDuration(ctx, input, duration, err == nil)
	return result, err
}

func (s Service) recordAnalyzerFailure(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	input ClaimInput,
	err error,
) error {
	var analyzerErr AnalyzerFailure
	if errors.As(err, &analyzerErr) {
		return s.recordFailure(ctx, mutation, input, analyzerErr.Disposition(), analyzerErr.FailureClass(), analyzerErr.ResourceUsage())
	}
	return s.recordFailure(ctx, mutation, input, FailureDeadLetter, FailureClassAnalyzerFailed, ResourceUsage{})
}

func (s Service) recordFailure(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	input ClaimInput,
	disposition FailureDisposition,
	failureClass FailureClass,
	usage ResourceUsage,
) error {
	payload, err := FailurePayloadFor(input, disposition, failureClass, usage)
	if err != nil {
		return err
	}
	failed := mutation
	failed.ObservedAt = s.now()
	failed.FailureClass = string(failureClass)
	failed.FailureMessage = payload.String()
	if disposition == FailureRetryable {
		failed.VisibleAt = s.now().Add(s.PollInterval)
		if err := s.ControlStore.FailClaimRetryable(ctx, failed); err != nil {
			return fmt.Errorf("retryable-fail scanner-worker claim: %w", err)
		}
		s.recordRetry(ctx, input, failureClass)
		s.logFailure("scanner-worker claim retryable failure", input, failureClass)
		return nil
	}
	if err := s.ControlStore.FailClaimTerminal(ctx, failed); err != nil {
		return fmt.Errorf("terminal-fail scanner-worker claim: %w", err)
	}
	s.recordDeadLetter(ctx, input, failureClass)
	s.logFailure("scanner-worker claim dead-lettered", input, failureClass)
	return nil
}

func (s Service) resourceLimits() (ResourceLimits, error) {
	if s.ResourceLimits == (ResourceLimits{}) {
		return DefaultResourceLimits(s.AnalyzerKind)
	}
	if err := s.ResourceLimits.validate(); err != nil {
		return ResourceLimits{}, err
	}
	return s.ResourceLimits, nil
}

func (s Service) claimMutation(item workflow.WorkItem, claim workflow.Claim) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:         item.WorkItemID,
		ClaimID:            claim.ClaimID,
		FencingToken:       claim.FencingToken,
		OwnerID:            claim.OwnerID,
		ObservedAt:         s.now(),
		LeaseDuration:      s.ClaimLeaseTTL,
		TenantID:           item.TenantID,
		WorkspaceID:        item.WorkspaceID,
		SubjectClass:       item.SubjectClass,
		PolicyRevisionHash: item.PolicyRevisionHash,
	}
}

func (s Service) startHeartbeatLoop(ctx context.Context, mutation workflow.ClaimMutation) <-chan error {
	errc := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(s.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				next := mutation
				next.ObservedAt = s.now()
				if err := s.ControlStore.HeartbeatClaim(ctx, next); err != nil {
					errc <- fmt.Errorf("heartbeat scanner-worker claim: %w", err)
					return
				}
			}
		}
	}()
	return errc
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func scopeAndGenerationForInput(input ClaimInput, observedAt time.Time) (scope.IngestionScope, scope.ScopeGeneration) {
	parentScopeID := strings.TrimSpace(input.Target.AcceptanceUnitID)
	if parentScopeID == strings.TrimSpace(input.Target.ScopeID) {
		parentScopeID = ""
	}
	return scope.IngestionScope{
			ScopeID:       input.Target.ScopeID,
			SourceSystem:  string(scope.CollectorScannerWorker),
			ScopeKind:     scope.KindScannerWorker,
			ParentScopeID: parentScopeID,
			CollectorKind: scope.CollectorScannerWorker,
			PartitionKey:  string(input.Analyzer) + ":" + input.Target.LocatorHash,
			Metadata: map[string]string{
				"analyzer":            string(input.Analyzer),
				"target_kind":         string(input.Target.Kind),
				"target_locator_hash": input.Target.LocatorHash,
			},
		}, scope.ScopeGeneration{
			GenerationID:  input.GenerationID,
			ScopeID:       input.Target.ScopeID,
			ObservedAt:    input.ObservedAt,
			IngestedAt:    observedAt.UTC(),
			Status:        scope.GenerationStatusPending,
			TriggerKind:   scope.TriggerKindSnapshot,
			FreshnessHint: fmt.Sprintf("scanner_worker:%s:%d", input.WorkItemID, input.FencingToken),
		}
}

func waitForNextScannerPoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func drainScannerHeartbeatError(errc <-chan error) error {
	select {
	case err := <-errc:
		return err
	default:
		return nil
	}
}

func (s Service) logFailure(message string, input ClaimInput, failureClass FailureClass) {
	if s.Logger == nil {
		return
	}
	s.Logger.Warn(
		message,
		telemetry.EventAttr("scanner_worker.claim.failure"),
		telemetry.FailureClassAttr(string(failureClass)),
		"analyzer", string(input.Analyzer),
		"target_kind", string(input.Target.Kind),
		"target_locator_hash", input.Target.LocatorHash,
		"work_item_id", input.WorkItemID,
		"claim_id", input.ClaimID,
	)
}

func (s Service) logCommitFailure(input ClaimInput, err error) {
	if s.Logger == nil {
		return
	}
	info := classifyCommitFailure(err)
	attrs := []any{
		telemetry.EventAttr("scanner_worker.claim.commit.failure"),
		telemetry.FailureClassAttr(string(FailureClassCommitFailed)),
		"commit_failure_class", info.Class,
		"analyzer", string(input.Analyzer),
		"target_kind", string(input.Target.Kind),
		"target_locator_hash", input.Target.LocatorHash,
		"work_item_id", input.WorkItemID,
		"claim_id", input.ClaimID,
	}
	if info.SQLState != "" {
		attrs = append(attrs, "commit_sqlstate", info.SQLState)
	}
	if info.Table != "" {
		attrs = append(attrs, "commit_table", info.Table)
	}
	if info.Constraint != "" {
		attrs = append(attrs, "commit_constraint", info.Constraint)
	}
	s.Logger.Warn("scanner-worker commit failed", attrs...)
}

type commitFailureInfo struct {
	Class      string
	SQLState   string
	Table      string
	Constraint string
}

func classifyCommitFailure(err error) commitFailureInfo {
	message := strings.ToLower(strings.TrimSpace(fmt.Sprint(err)))
	info := commitFailureInfo{Class: "unknown"}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		info.SQLState = strings.TrimSpace(pgErr.Code)
		info.Table = strings.TrimSpace(pgErr.TableName)
		info.Constraint = strings.TrimSpace(pgErr.ConstraintName)
		info.Class = classifyPostgresCommitFailure(pgErr.Code)
	}
	switch {
	case strings.Contains(message, "check active generation freshness"):
		info.Class = "freshness_check"
	case strings.Contains(message, "ingested_at must not be before observed_at"),
		strings.Contains(message, "parent_scope_id must differ from scope_id"),
		strings.Contains(message, "generation scope_id"),
		strings.Contains(message, "must not be terminal before projection"):
		info.Class = "generation_validation"
	case strings.Contains(message, "fact store database is required"),
		strings.Contains(message, "fact ") && strings.Contains(message, " scope_id "),
		strings.Contains(message, "fact ") && strings.Contains(message, " generation_id "),
		strings.Contains(message, "observed_at must not be zero"),
		strings.Contains(message, "schema_version must be semantic version"),
		strings.Contains(message, "source_confidence"):
		info.Class = "fact_validation"
	case strings.Contains(message, "verify active workflow claim"):
		info.Class = "claim_fence"
	case strings.Contains(message, "transaction beginner"),
		strings.Contains(message, "begin ingestion transaction"):
		info.Class = "transaction_begin"
	case strings.Contains(message, "upsert ingestion scope"):
		info.Class = "ingestion_scope"
	case strings.Contains(message, "upsert scope generation"):
		info.Class = "scope_generation"
	case strings.Contains(message, "load repository catalog"):
		info.Class = "repository_catalog"
	case strings.Contains(message, "upsert fact batch"),
		strings.Contains(message, "schema_version"),
		strings.Contains(message, "read fact stream"):
		info.Class = "fact_persistence"
	case strings.Contains(message, "relationship evidence"),
		strings.Contains(message, "relationship_backfill"):
		info.Class = "relationship_evidence"
	case strings.Contains(message, "enqueue projector work"):
		info.Class = "projector_enqueue"
	case strings.Contains(message, "commit ingestion transaction"):
		info.Class = "transaction_commit"
	}
	return info
}

func classifyPostgresCommitFailure(code string) string {
	switch strings.TrimSpace(code) {
	case "23503":
		return "database_foreign_key"
	case "23505":
		return "database_unique_violation"
	case "23502":
		return "database_not_null"
	case "21000":
		return "database_cardinality_violation"
	case "40001":
		return "database_serialization_failure"
	case "40P01":
		return "database_deadlock"
	default:
		return "database_error"
	}
}
