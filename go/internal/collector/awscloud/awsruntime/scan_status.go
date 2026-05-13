package awsruntime

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (s ClaimedSource) startScanStatus(ctx context.Context, boundary awscloud.Boundary) error {
	if s.ScanStatus == nil {
		return nil
	}
	if err := s.ScanStatus.StartAWSScan(ctx, awscloud.ScanStatusStart{
		Boundary:  boundary,
		StartedAt: s.now(),
	}); err != nil {
		return fmt.Errorf("start AWS scan status: %w", err)
	}
	return nil
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
	} else if scanErr != nil {
		statusValue = awscloud.ScanStatusFailed
		failureClass = awsScanFailureClass(apiStats)
		failureMessage = awscloud.SanitizeScanStatusMessage(scanErr.Error())
	}
	if factStats.BudgetExhausted && s.Instruments != nil && s.Instruments.AWSBudgetExhausted != nil {
		s.Instruments.AWSBudgetExhausted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(boundary.ServiceKind),
			telemetry.AttrAccount(boundary.AccountID),
			telemetry.AttrRegion(boundary.Region),
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
		return fmt.Errorf("observe AWS scan status: %w", err)
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
			}
		}
	}
	return stats
}

func awsScanFailureClass(stats awscloud.APICallStats) string {
	if stats.ThrottleCount > 0 {
		return "throttled"
	}
	return "collect_failure"
}
