// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// MaxLimit bounds how many eligible nodes one request may retire and how
	// many ids it may name.
	MaxLimit = legacyworkload.MaxBatch
	// maxBlockedListed bounds the blocked nodes one response lists.
	maxBlockedListed = 100
	// maxExamined bounds the candidates one request reads and evaluates.
	maxExamined = 1000
)

// Outcome label values for eshu_dp_workload_legacy_retirements_total.
const (
	OutcomeDeleted              = "deleted"
	OutcomeBlockedHasInstances  = "blocked_has_instances"
	OutcomeBlockedNoTwin        = "blocked_no_twin"
	OutcomeBlockedRelationships = "blocked_relationships"
)

// Governance audit reason codes for the execute decisions.
const (
	reasonExecuted                = "legacy_workload_retire_executed"
	reasonFailed                  = "legacy_workload_retire_failed"
	reasonRefusedMissingReason    = "legacy_workload_retire_refused_missing_reason"
	reasonRefusedUnsupportedGraph = "legacy_workload_retire_refused_unsupported_backend"
)

var (
	// ErrInvalidRequest wraps a caller-fixable request problem; its text is
	// safe to return to the caller.
	ErrInvalidRequest = errors.New("invalid legacy workload retirement request")
	// ErrExecuteUnsupported reports that the configured graph backend cannot
	// evaluate the retirement statement. Dry runs still work.
	ErrExecuteUnsupported = errors.New("legacy workload retirement execute is supported on Neo4j only; run a dry run to list candidates")
)

// Writer is the graph write port: one auto-commit statement, returning the
// nodes its committed attempts deleted from the backend write summary. The
// reducer's retrying Cypher executor satisfies it, so transient errors and
// deadlocks retry through the shared classification.
type Writer interface {
	ExecuteCypherCountingNodeDeletes(ctx context.Context, cypher string, params map[string]any) (int64, error)
}

// Auditor appends governance audit events. A nil Auditor disables recording.
type Auditor interface {
	Append(ctx context.Context, events []governanceaudit.Event) error
}

// Runner detects and retires pre-#5385 name-only Workload nodes. It is the
// only code that executes the retirement statement.
type Runner struct {
	// Reader runs the bounded detection reads.
	Reader legacyworkload.Reader
	// Writer runs the retirement statement; required for execute.
	Writer Writer
	// ExecuteSupported is true on the graph backend that can evaluate
	// legacyworkload.DeleteNeo4jCypher (Neo4j). When false, execute is refused.
	ExecuteSupported bool
	// Audit records execute decisions; nil disables recording.
	Audit Auditor
	// Instruments carries the retirement counter; nil skips it.
	Instruments *telemetry.Instruments
	// Logger receives the per-request INFO line and per-node WARN lines; nil
	// uses slog.Default.
	Logger *slog.Logger
	// Now supplies audit timestamps; nil uses time.Now.
	Now func() time.Time

	// mu serializes requests in this process. It does not hold a queue lane:
	// it only stops two operator requests from reading and deleting the same
	// batch at once.
	mu sync.Mutex
}

// Request is one retirement request.
type Request struct {
	DryRun bool
	// Limit bounds the eligible nodes returned or deleted; zero means MaxLimit.
	Limit int
	// IDs optionally restricts the request to these node ids.
	IDs []string
	// Reason is required when DryRun is false and goes to the log line.
	Reason string
}

// Candidate is one name-only Workload node in a response.
type Candidate struct {
	ID            string         `json:"id"`
	TwinID        string         `json:"twin_id"`
	RepoID        string         `json:"repo_id"`
	Name          string         `json:"name"`
	Relationships map[string]int `json:"relationships"`
	Eligible      bool           `json:"eligible"`
	BlockedReason string         `json:"blocked_reason"`
}

// Response is the result of a request.
type Response struct {
	DryRun     bool        `json:"dry_run"`
	Candidates []Candidate `json:"candidates"`
	Eligible   int         `json:"eligible"`
	Blocked    int         `json:"blocked"`
	Deleted    int         `json:"deleted"`
	// Truncated is true when the request stopped before the label was
	// exhausted, so more candidates may remain.
	Truncated bool `json:"truncated"`
}

// Run executes one request.
func (r *Runner) Run(ctx context.Context, req Request) (Response, error) {
	req.Reason = strings.TrimSpace(req.Reason)
	if err := validate(req); err != nil {
		if !req.DryRun && errors.Is(err, errReasonRequired) {
			r.audit(ctx, governanceaudit.DecisionDenied, reasonRefusedMissingReason)
		}
		return Response{}, err
	}
	if req.Limit == 0 {
		req.Limit = MaxLimit
	}
	if !req.DryRun && (!r.ExecuteSupported || r.Writer == nil) {
		r.audit(ctx, governanceaudit.DecisionDenied, reasonRefusedUnsupportedGraph)
		return Response{}, ErrExecuteUnsupported
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	report, err := legacyworkload.Detect(ctx, r.Reader, legacyworkload.Options{
		IDs:         req.IDs,
		MaxEligible: req.Limit,
		MaxExamined: maxExamined,
	})
	if err != nil {
		return Response{}, fmt.Errorf("detect legacy workloads: %w", err)
	}
	resp, eligibleIDs := buildResponse(report, req)
	if req.DryRun {
		return resp, nil
	}

	deleted, err := r.deleteBatch(ctx, eligibleIDs)
	if err != nil {
		r.audit(ctx, governanceaudit.DecisionUnavailable, reasonFailed)
		r.logger().ErrorContext(ctx, "legacy workload retirement failed",
			slog.Int("eligible", resp.Eligible), slog.String("reason", req.Reason), slog.String("error", err.Error()))
		return Response{}, fmt.Errorf("retire legacy workloads: %w", err)
	}
	resp.Deleted = deleted
	r.record(ctx, resp, req.Reason)
	r.audit(ctx, governanceaudit.DecisionAllowed, reasonExecuted)
	return resp, nil
}

var errReasonRequired = fmt.Errorf("%w: reason is required when dry_run is false", ErrInvalidRequest)

func validate(req Request) error {
	if req.Limit < 0 || req.Limit > MaxLimit {
		return fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidRequest, MaxLimit)
	}
	if len(req.IDs) > MaxLimit {
		return fmt.Errorf("%w: ids may hold at most %d entries", ErrInvalidRequest, MaxLimit)
	}
	if slices.ContainsFunc(req.IDs, func(id string) bool { return strings.TrimSpace(id) == "" }) {
		return fmt.Errorf("%w: ids must not hold a blank entry", ErrInvalidRequest)
	}
	if !req.DryRun && req.Reason == "" {
		return errReasonRequired
	}
	return nil
}

// buildResponse lists up to Limit eligible nodes and up to maxBlockedListed
// blocked ones, and returns the ids to delete.
func buildResponse(report legacyworkload.Report, req Request) (Response, []string) {
	resp := Response{DryRun: req.DryRun, Candidates: []Candidate{}, Truncated: report.Truncated}
	var eligibleIDs []string
	for _, v := range report.Verdicts {
		switch {
		case v.Eligible && len(eligibleIDs) < req.Limit:
			eligibleIDs = append(eligibleIDs, v.ID)
		case v.Eligible:
			resp.Truncated = true
			continue
		case resp.Blocked >= maxBlockedListed:
			resp.Truncated = true
			continue
		}
		if v.Eligible {
			resp.Eligible++
		} else {
			resp.Blocked++
		}
		resp.Candidates = append(resp.Candidates, Candidate{
			ID: v.ID, TwinID: v.TwinID, RepoID: v.RepoID, Name: v.Name,
			Relationships: v.Relationships, Eligible: v.Eligible, BlockedReason: v.BlockedReason,
		})
	}
	return resp, eligibleIDs
}

// deleteBatch runs the single retirement statement over the eligible ids. The
// statement re-evaluates every clause, so ids that stopped being eligible since
// the read are simply not matched.
func (r *Runner) deleteBatch(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	n, err := r.Writer.ExecuteCypherCountingNodeDeletes(ctx, legacyworkload.DeleteNeo4jCypher, map[string]any{"ids": ids})
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// record emits the counter and the log lines for an executed request.
func (r *Runner) record(ctx context.Context, resp Response, reason string) {
	blocked := map[string]int{}
	for _, c := range resp.Candidates {
		if c.Eligible {
			continue
		}
		blocked[c.BlockedReason]++
		r.logger().WarnContext(ctx, "legacy workload kept",
			slog.String("workload_id", c.ID), slog.String("blocked_reason", c.BlockedReason),
			slog.Any("relationship_counts", c.Relationships))
	}
	r.count(ctx, OutcomeDeleted, resp.Deleted)
	r.count(ctx, OutcomeBlockedHasInstances, blocked[legacyworkload.BlockedHasInstances])
	r.count(ctx, OutcomeBlockedNoTwin, blocked[legacyworkload.BlockedNoTwin])
	r.count(ctx, OutcomeBlockedRelationships, blocked[legacyworkload.BlockedRelationships])
	if resp.Deleted < resp.Eligible {
		r.logger().WarnContext(ctx, "legacy workload retirement deleted fewer nodes than were eligible",
			slog.Int("eligible", resp.Eligible), slog.Int("deleted", resp.Deleted))
	}
	r.logger().InfoContext(ctx, "legacy workload retirement executed",
		slog.Int("candidates", len(resp.Candidates)), slog.Int("eligible", resp.Eligible),
		slog.Int("deleted", resp.Deleted), slog.Int("blocked", resp.Blocked),
		slog.Bool("truncated", resp.Truncated), slog.String("reason", reason))
}

func (r *Runner) count(ctx context.Context, outcome string, n int) {
	if n <= 0 || r.Instruments == nil || r.Instruments.WorkloadLegacyRetirements == nil {
		return
	}
	r.Instruments.WorkloadLegacyRetirements.Add(ctx, int64(n), metric.WithAttributes(attribute.String("outcome", outcome)))
}

func (r *Runner) audit(ctx context.Context, decision governanceaudit.Decision, reasonCode string) {
	if r.Audit == nil {
		return
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	event := governanceaudit.Event{
		Type:       governanceaudit.EventTypeAdminRecoveryAction,
		ActorClass: governanceaudit.ActorClassAnonymous,
		ScopeClass: governanceaudit.ScopeClassAdmin,
		Decision:   decision,
		ReasonCode: reasonCode,
		OccurredAt: now().UTC(),
	}
	appendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := r.Audit.Append(appendCtx, []governanceaudit.Event{event}); err != nil {
		r.logger().WarnContext(ctx, "legacy workload retirement audit append failed", slog.String("error", err.Error()))
	}
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}
