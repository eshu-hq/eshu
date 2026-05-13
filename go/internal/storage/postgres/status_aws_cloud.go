package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const awsCloudScanStatusQuery = `
SELECT
    collector_instance_id,
    account_id,
    region,
    service_kind,
    status,
    commit_status,
    failure_class,
    failure_message,
    api_call_count,
    throttle_count,
    warning_count,
    resource_count,
    relationship_count,
    tag_observation_count,
    budget_exhausted,
    credential_failed,
    last_started_at,
    last_observed_at,
    last_completed_at,
    last_successful_at,
    updated_at
FROM aws_scan_status
ORDER BY collector_instance_id ASC, account_id ASC, region ASC, service_kind ASC
LIMIT 200
`

func readAWSCloudScanStatuses(ctx context.Context, queryer Queryer) ([]statuspkg.AWSCloudScanStatus, error) {
	rows, err := queryer.QueryContext(ctx, awsCloudScanStatusQuery)
	if err != nil {
		return nil, fmt.Errorf("list AWS cloud scan statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	output := []statuspkg.AWSCloudScanStatus{}
	for rows.Next() {
		var row statuspkg.AWSCloudScanStatus
		var lastStartedAt sql.NullTime
		var lastObservedAt sql.NullTime
		var lastCompletedAt sql.NullTime
		var lastSuccessfulAt sql.NullTime
		if err := rows.Scan(
			&row.CollectorInstanceID,
			&row.AccountID,
			&row.Region,
			&row.ServiceKind,
			&row.Status,
			&row.CommitStatus,
			&row.FailureClass,
			&row.FailureMessage,
			&row.APICallCount,
			&row.ThrottleCount,
			&row.WarningCount,
			&row.ResourceCount,
			&row.RelationshipCount,
			&row.TagObservationCount,
			&row.BudgetExhausted,
			&row.CredentialFailed,
			&lastStartedAt,
			&lastObservedAt,
			&lastCompletedAt,
			&lastSuccessfulAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("list AWS cloud scan statuses: %w", err)
		}
		row.LastStartedAt = nullableTimeUTC(lastStartedAt)
		row.LastObservedAt = nullableTimeUTC(lastObservedAt)
		row.LastCompletedAt = nullableTimeUTC(lastCompletedAt)
		row.LastSuccessfulAt = nullableTimeUTC(lastSuccessfulAt)
		row.UpdatedAt = row.UpdatedAt.UTC()
		output = append(output, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list AWS cloud scan statuses: %w", err)
	}
	return output, nil
}

func nullableTimeUTC(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}
