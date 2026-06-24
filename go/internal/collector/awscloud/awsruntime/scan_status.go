// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// FailureClassStaleFence labels classified AWS scan-status stale-fence
// failures on metrics and workflow_claims rows. Operators read this label to
// separate stale-fence terminal failures from credential, throttle, and
// network-class collector failures.
const FailureClassStaleFence = "stale_fence"

// FailureClassPermissionDenied labels AWS service scans that reached AWS and
// were rejected by an access-denied or unauthorized API response. The error is
// terminal for the claimed scope until the operator fixes IAM permissions.
const FailureClassPermissionDenied = "permission_denied"

// FailureClassUnsupportedPermission labels AWS service scans that reached AWS
// and were rejected because the requested metadata operation is unsupported for
// that scope. Service adapters should absorb optional unsupported reads; an
// unhandled top-level unsupported operation is terminal for the claim.
const FailureClassUnsupportedPermission = "unsupported_permission"

// ScanStatusPhaseStart, ScanStatusPhaseObserve, and ScanStatusPhaseCommit are
// the closed set of values for the `operation` attribute on
// eshu_dp_aws_scan_status_stale_fence_total. Producers MUST use these
// constants so the metric's documented label cardinality stays in lockstep
// with the operations actually emitted.
const (
	ScanStatusPhaseStart   = "start"
	ScanStatusPhaseObserve = "observe"
	ScanStatusPhaseCommit  = "commit"
)

// scanStatusStaleFenceError marks a status-store rejection as terminal so the
// ClaimedService runner stops looping the claim through the retryable queue.
// Issue #612: an orphaned aws_scan_status row used to block every future
// generation for the same per-target slot, and the retry loop drove
// workflow_claims.failed_retryable into the millions on platform-qa.
type scanStatusStaleFenceError struct {
	err error
}

func (e scanStatusStaleFenceError) Error() string { return e.err.Error() }

func (e scanStatusStaleFenceError) Unwrap() error { return e.err }

func (e scanStatusStaleFenceError) FailureClass() string { return FailureClassStaleFence }

func (e scanStatusStaleFenceError) TerminalFailure() bool { return true }

type terminalServiceScanError struct {
	err          error
	failureClass string
}

func (e terminalServiceScanError) Error() string { return e.err.Error() }

func (e terminalServiceScanError) Unwrap() error { return e.err }

func (e terminalServiceScanError) FailureClass() string { return e.failureClass }

func (e terminalServiceScanError) TerminalFailure() bool { return true }

// ClassifyScanStatusStaleFence inspects err for awscloud.ErrScanStatusStaleFence
// and, when found, records eshu_dp_aws_scan_status_stale_fence_total and
// returns a typed terminal failure so the ClaimedService runner routes the
// claim to FailClaimTerminal. err is returned unchanged when it is nil or not
// a stale-fence rejection. Callers MUST pass one of ScanStatusPhaseStart,
// ScanStatusPhaseObserve, or ScanStatusPhaseCommit as phase so the metric's
// operation label stays bounded.
func ClassifyScanStatusStaleFence(
	ctx context.Context,
	err error,
	instruments *telemetry.Instruments,
	boundary awscloud.Boundary,
	phase string,
) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, awscloud.ErrScanStatusStaleFence) {
		return err
	}
	recordScanStatusStaleFence(ctx, instruments, boundary, phase)
	return scanStatusStaleFenceError{err: err}
}

func recordScanStatusStaleFence(
	ctx context.Context,
	instruments *telemetry.Instruments,
	boundary awscloud.Boundary,
	phase string,
) {
	if instruments == nil || instruments.AWSScanStatusStaleFence == nil {
		return
	}
	instruments.AWSScanStatusStaleFence.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrService(boundary.ServiceKind),
		telemetry.AttrAccount(boundary.AccountID),
		telemetry.AttrRegion(boundary.Region),
		telemetry.AttrOperation(phase),
	))
}

func (s ClaimedSource) startScanStatus(ctx context.Context, boundary awscloud.Boundary) error {
	if s.ScanStatus == nil {
		return nil
	}
	if err := s.ScanStatus.StartAWSScan(ctx, awscloud.ScanStatusStart{
		Boundary:  boundary,
		StartedAt: s.now(),
	}); err != nil {
		return ClassifyScanStatusStaleFence(
			ctx,
			fmt.Errorf("start AWS scan status: %w", err),
			s.Instruments,
			boundary,
			ScanStatusPhaseStart,
		)
	}
	return nil
}

func classifyServiceScanError(err error) error {
	if err == nil {
		return nil
	}
	if failureClass := terminalServiceScanFailureClass(err); failureClass != "" {
		return terminalServiceScanError{
			err:          err,
			failureClass: failureClass,
		}
	}
	return err
}

func terminalServiceScanFailureClass(err error) string {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return ""
	}
	switch strings.TrimSpace(apiErr.ErrorCode()) {
	case "AccessDenied", "AccessDeniedException", "UnauthorizedOperation", "UnauthorizedException", "ForbiddenException":
		return FailureClassPermissionDenied
	case "UnsupportedOperation", "UnsupportedOperationException":
		return FailureClassUnsupportedPermission
	default:
		return ""
	}
}

func (s ClaimedSource) observeScanStatus(
	ctx context.Context,
	boundary awscloud.Boundary,
	apiStats awscloud.APICallStats,
	envelopes []facts.Envelope,
	scanErr error,
) error {
	if s.ScanStatus == nil {
		return nil
	}
	factStats := awsFactStats(envelopes)
	statusValue := awscloud.ScanStatusSucceeded
	failureClass := ""
	failureMessage := ""
	if factStats.CredentialFailed {
		statusValue = awscloud.ScanStatusCredentialFailed
		failureClass = "creds_broken"
	} else if factStats.BudgetExhausted {
		statusValue = awscloud.ScanStatusPartial
		failureClass = "budget_exhausted"
	} else if factStats.Throttled {
		statusValue = awscloud.ScanStatusPartial
		failureClass = "throttled"
	} else if factStats.OrgAccessSkipped {
		statusValue = awscloud.ScanStatusPartial
		failureClass = "org_access_skipped"
	} else if scanErr != nil {
		statusValue = awscloud.ScanStatusFailed
		failureClass = awsScanFailureClass(apiStats, scanErr)
		failureMessage = awscloud.SanitizeScanStatusMessage(scanErr.Error())
	}
	if factStats.BudgetExhausted && s.Instruments != nil && s.Instruments.AWSBudgetExhausted != nil {
		s.Instruments.AWSBudgetExhausted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(boundary.ServiceKind),
			telemetry.AttrAccount(boundary.AccountID),
			telemetry.AttrRegion(boundary.Region),
		))
	}
	if factStats.OrgAccessSkipped && s.Instruments != nil && s.Instruments.AWSOrgAccessSkipped != nil {
		s.Instruments.AWSOrgAccessSkipped.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(boundary.ServiceKind),
			telemetry.AttrAccount(boundary.AccountID),
			telemetry.AttrRegion(boundary.Region),
			telemetry.AttrReason(firstNonEmpty(factStats.OrgAccessSkipReason, "unknown")),
		))
	}
	if err := s.ScanStatus.ObserveAWSScan(ctx, awscloud.ScanStatusObservation{
		Boundary:            boundary,
		Status:              statusValue,
		FailureClass:        failureClass,
		FailureMessage:      failureMessage,
		APICallCount:        apiStats.APICallCount,
		ThrottleCount:       apiStats.ThrottleCount,
		WarningCount:        factStats.WarningCount,
		ResourceCount:       factStats.ResourceCount,
		RelationshipCount:   factStats.RelationshipCount,
		TagObservationCount: factStats.TagObservationCount,
		BudgetExhausted:     factStats.BudgetExhausted,
		CredentialFailed:    factStats.CredentialFailed,
		ObservedAt:          s.now(),
	}); err != nil {
		return ClassifyScanStatusStaleFence(
			ctx,
			fmt.Errorf("observe AWS scan status: %w", err),
			s.Instruments,
			boundary,
			ScanStatusPhaseObserve,
		)
	}
	return nil
}

type awsEnvelopeStats struct {
	WarningCount        int
	ResourceCount       int
	RelationshipCount   int
	TagObservationCount int
	BudgetExhausted     bool
	CredentialFailed    bool
	Throttled           bool
	OrgAccessSkipped    bool
	OrgAccessSkipReason string
}

func awsFactStats(envelopes []facts.Envelope) awsEnvelopeStats {
	var stats awsEnvelopeStats
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.AWSResourceFactKind:
			stats.ResourceCount++
		case facts.AWSRelationshipFactKind:
			stats.RelationshipCount++
		case facts.AWSTagObservationFactKind:
			stats.TagObservationCount++
		case facts.AWSWarningFactKind:
			stats.WarningCount++
			warningKind, _ := envelope.Payload["warning_kind"].(string)
			switch strings.TrimSpace(warningKind) {
			case awscloud.WarningBudgetExhausted:
				stats.BudgetExhausted = true
			case awscloud.WarningAssumeRoleFailed:
				stats.CredentialFailed = true
			case awscloud.WarningThrottleSustained:
				stats.Throttled = true
			case awscloud.WarningOrganizationsOrgAccessSkipped:
				stats.OrgAccessSkipped = true
				stats.OrgAccessSkipReason = warningSkipReason(envelope)
			}
		}
	}
	return stats
}

func warningSkipReason(envelope facts.Envelope) string {
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		return ""
	}
	reason, _ := attributes["skip_reason"].(string)
	return strings.TrimSpace(reason)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func awsScanFailureClass(stats awscloud.APICallStats, err error) string {
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		if value := strings.TrimSpace(classified.FailureClass()); value != "" {
			return value
		}
	}
	if stats.ThrottleCount > 0 {
		return "throttled"
	}
	return "collect_failure"
}
