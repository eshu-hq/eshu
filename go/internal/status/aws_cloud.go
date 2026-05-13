package status

import (
	"fmt"
	"slices"
	"time"
)

// AWSCloudScanStatus captures one AWS collector tuple status row for
// `(collector_instance_id, account_id, region, service_kind)`.
type AWSCloudScanStatus struct {
	CollectorInstanceID string
	AccountID           string
	Region              string
	ServiceKind         string
	Status              string
	CommitStatus        string
	FailureClass        string
	FailureMessage      string
	APICallCount        int
	ThrottleCount       int
	WarningCount        int
	ResourceCount       int
	RelationshipCount   int
	TagObservationCount int
	BudgetExhausted     bool
	CredentialFailed    bool
	LastStartedAt       time.Time
	LastObservedAt      time.Time
	LastCompletedAt     time.Time
	LastSuccessfulAt    time.Time
	UpdatedAt           time.Time
}

func cloneAWSCloudScanStatuses(rows []AWSCloudScanStatus) []AWSCloudScanStatus {
	return slices.Clone(rows)
}

func renderAWSCloudScanLines(rows []AWSCloudScanStatus) []string {
	if len(rows) == 0 {
		return nil
	}
	lines := []string{"AWS cloud scans:"}
	for _, row := range rows {
		line := fmt.Sprintf(
			"  %s %s/%s/%s status=%s commit=%s api_calls=%d throttles=%d warnings=%d resources=%d relationships=%d tags=%d",
			row.CollectorInstanceID,
			row.AccountID,
			row.Region,
			row.ServiceKind,
			row.Status,
			row.CommitStatus,
			row.APICallCount,
			row.ThrottleCount,
			row.WarningCount,
			row.ResourceCount,
			row.RelationshipCount,
			row.TagObservationCount,
		)
		if row.FailureClass != "" {
			line += fmt.Sprintf(" failure=%s", row.FailureClass)
		}
		if row.BudgetExhausted {
			line += " budget_exhausted=true"
		}
		if row.CredentialFailed {
			line += " credential_failed=true"
		}
		if !row.LastCompletedAt.IsZero() {
			line += fmt.Sprintf(" last_completed_at=%s", row.LastCompletedAt.UTC().Format(time.RFC3339))
		}
		lines = append(lines, line)
	}
	return lines
}
