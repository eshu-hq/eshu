package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

const awsScanStatusSchemaSQL = `
CREATE TABLE IF NOT EXISTS aws_scan_status (
    collector_instance_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    region TEXT NOT NULL,
    service_kind TEXT NOT NULL,
    scope_id TEXT NOT NULL DEFAULT '',
    generation_id TEXT NOT NULL DEFAULT '',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    commit_status TEXT NOT NULL,
    failure_class TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    api_call_count INTEGER NOT NULL DEFAULT 0,
    throttle_count INTEGER NOT NULL DEFAULT 0,
    warning_count INTEGER NOT NULL DEFAULT 0,
    resource_count INTEGER NOT NULL DEFAULT 0,
    relationship_count INTEGER NOT NULL DEFAULT 0,
    tag_observation_count INTEGER NOT NULL DEFAULT 0,
    budget_exhausted BOOLEAN NOT NULL DEFAULT FALSE,
    credential_failed BOOLEAN NOT NULL DEFAULT FALSE,
    last_started_at TIMESTAMPTZ NULL,
    last_observed_at TIMESTAMPTZ NULL,
    last_completed_at TIMESTAMPTZ NULL,
    last_successful_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (collector_instance_id, account_id, region, service_kind)
);

CREATE INDEX IF NOT EXISTS aws_scan_status_status_idx
    ON aws_scan_status (status, commit_status, updated_at DESC);

CREATE INDEX IF NOT EXISTS aws_scan_status_tuple_updated_idx
    ON aws_scan_status (
        collector_instance_id,
        account_id,
        region,
        service_kind,
        updated_at DESC
    );
`

const startAWSScanStatusQuery = `
INSERT INTO aws_scan_status (
    collector_instance_id,
    account_id,
    region,
    service_kind,
    scope_id,
    generation_id,
    fencing_token,
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
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, '', '', 0, 0, 0, 0, 0, 0, FALSE, FALSE, $10, NULL, NULL, NULL, $10
)
ON CONFLICT (collector_instance_id, account_id, region, service_kind) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    fencing_token = EXCLUDED.fencing_token,
    status = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.status ELSE EXCLUDED.status END,
    commit_status = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.commit_status ELSE EXCLUDED.commit_status END,
    failure_class = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.failure_class ELSE '' END,
    failure_message = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.failure_message ELSE '' END,
    api_call_count = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.api_call_count ELSE 0 END,
    throttle_count = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.throttle_count ELSE 0 END,
    warning_count = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.warning_count ELSE 0 END,
    resource_count = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.resource_count ELSE 0 END,
    relationship_count = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.relationship_count ELSE 0 END,
    tag_observation_count = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.tag_observation_count ELSE 0 END,
    budget_exhausted = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.budget_exhausted ELSE FALSE END,
    credential_failed = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.credential_failed ELSE FALSE END,
    last_started_at = EXCLUDED.last_started_at,
    last_observed_at = CASE WHEN aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
        THEN aws_scan_status.last_observed_at ELSE NULL END,
    last_completed_at = aws_scan_status.last_completed_at,
    last_successful_at = aws_scan_status.last_successful_at,
    updated_at = EXCLUDED.updated_at
WHERE aws_scan_status.fencing_token < EXCLUDED.fencing_token
   OR (
        aws_scan_status.generation_id = EXCLUDED.generation_id
        AND aws_scan_status.fencing_token = EXCLUDED.fencing_token
   )
`

const observeAWSScanStatusQuery = `
UPDATE aws_scan_status
SET
    status = $8,
    failure_class = $9,
    failure_message = $10,
    api_call_count = $11,
    throttle_count = $12,
    warning_count = $13,
    resource_count = $14,
    relationship_count = $15,
    tag_observation_count = $16,
    budget_exhausted = $17,
    credential_failed = $18,
    last_observed_at = $19,
    updated_at = $19
WHERE collector_instance_id = $1
  AND account_id = $2
  AND region = $3
  AND service_kind = $4
  AND generation_id = $5
  AND fencing_token = $6
`

const commitAWSScanStatusQuery = `
UPDATE aws_scan_status
SET
    commit_status = $8,
    failure_class = CASE WHEN $9 = '' THEN failure_class ELSE $9 END,
    failure_message = CASE WHEN $10 = '' THEN failure_message ELSE $10 END,
    last_completed_at = CASE WHEN $8 = 'committed' THEN $11 ELSE last_completed_at END,
    last_successful_at = CASE WHEN $8 = 'committed' AND status = 'succeeded' THEN $11 ELSE last_successful_at END,
    updated_at = $11
WHERE collector_instance_id = $1
  AND account_id = $2
  AND region = $3
  AND service_kind = $4
  AND generation_id = $5
  AND fencing_token = $6
`

// AWSScanStatusStore persists per-tuple AWS scan status for admin surfaces.
type AWSScanStatusStore struct {
	db  ExecQueryer
	Now func() time.Time
}

// NewAWSScanStatusStore constructs the AWS scan-status store.
func NewAWSScanStatusStore(db ExecQueryer) AWSScanStatusStore {
	return AWSScanStatusStore{db: db}
}

// AWSScanStatusSchemaSQL returns the DDL for AWS scan-status rows.
func AWSScanStatusSchemaSQL() string {
	return awsScanStatusSchemaSQL
}

func awsScanStatusBootstrapDefinition() Definition {
	return Definition{
		Name: "aws_scan_status",
		Path: "schema/data-plane/postgres/019_aws_scan_status.sql",
		SQL:  awsScanStatusSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, awsScanStatusBootstrapDefinition())
}

// EnsureSchema applies the AWS scan-status DDL.
func (s AWSScanStatusStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("aws scan status database is required")
	}
	if _, err := s.db.ExecContext(ctx, awsScanStatusSchemaSQL); err != nil {
		return fmt.Errorf("ensure AWS scan status schema: %w", err)
	}
	return nil
}

// StartAWSScan records a running AWS claim before credentials or API calls.
func (s AWSScanStatusStore) StartAWSScan(ctx context.Context, start awscloud.ScanStatusStart) error {
	if s.db == nil {
		return fmt.Errorf("aws scan status database is required")
	}
	if err := validateAWSScanBoundary(start.Boundary); err != nil {
		return err
	}
	startedAt := start.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = s.now()
	}
	result, err := s.db.ExecContext(
		ctx,
		startAWSScanStatusQuery,
		start.Boundary.CollectorInstanceID,
		start.Boundary.AccountID,
		start.Boundary.Region,
		start.Boundary.ServiceKind,
		start.Boundary.ScopeID,
		start.Boundary.GenerationID,
		start.Boundary.FencingToken,
		awscloud.ScanStatusRunning,
		awscloud.ScanCommitPending,
		startedAt,
	)
	if err != nil {
		return fmt.Errorf("start AWS scan status: %w", err)
	}
	return validateAWSScanStatusMutation(result)
}

// ObserveAWSScan records scanner-side completion evidence for a claim.
func (s AWSScanStatusStore) ObserveAWSScan(ctx context.Context, observation awscloud.ScanStatusObservation) error {
	if s.db == nil {
		return fmt.Errorf("aws scan status database is required")
	}
	if err := validateAWSScanBoundary(observation.Boundary); err != nil {
		return err
	}
	observedAt := observation.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = s.now()
	}
	result, err := s.db.ExecContext(
		ctx,
		observeAWSScanStatusQuery,
		observation.Boundary.CollectorInstanceID,
		observation.Boundary.AccountID,
		observation.Boundary.Region,
		observation.Boundary.ServiceKind,
		observation.Boundary.GenerationID,
		observation.Boundary.FencingToken,
		strings.TrimSpace(observation.Status),
		strings.TrimSpace(observation.FailureClass),
		awscloud.SanitizeScanStatusMessage(observation.FailureMessage),
		observation.APICallCount,
		observation.ThrottleCount,
		observation.WarningCount,
		observation.ResourceCount,
		observation.RelationshipCount,
		observation.TagObservationCount,
		observation.BudgetExhausted,
		observation.CredentialFailed,
		observedAt,
	)
	if err != nil {
		return fmt.Errorf("observe AWS scan status: %w", err)
	}
	return validateAWSScanStatusMutation(result)
}

// CommitAWSScan records the durable fact-commit outcome for a claim.
func (s AWSScanStatusStore) CommitAWSScan(ctx context.Context, commit awscloud.ScanStatusCommit) error {
	if s.db == nil {
		return fmt.Errorf("aws scan status database is required")
	}
	if err := validateAWSScanBoundary(commit.Boundary); err != nil {
		return err
	}
	completedAt := commit.CompletedAt.UTC()
	if completedAt.IsZero() {
		completedAt = s.now()
	}
	result, err := s.db.ExecContext(
		ctx,
		commitAWSScanStatusQuery,
		commit.Boundary.CollectorInstanceID,
		commit.Boundary.AccountID,
		commit.Boundary.Region,
		commit.Boundary.ServiceKind,
		commit.Boundary.GenerationID,
		commit.Boundary.FencingToken,
		strings.TrimSpace(commit.CommitStatus),
		strings.TrimSpace(commit.FailureClass),
		awscloud.SanitizeScanStatusMessage(commit.FailureMessage),
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("commit AWS scan status: %w", err)
	}
	return validateAWSScanStatusMutation(result)
}

func validateAWSScanBoundary(boundary awscloud.Boundary) error {
	switch {
	case strings.TrimSpace(boundary.CollectorInstanceID) == "":
		return fmt.Errorf("aws scan status requires collector_instance_id")
	case strings.TrimSpace(boundary.AccountID) == "":
		return fmt.Errorf("aws scan status requires account_id")
	case strings.TrimSpace(boundary.Region) == "":
		return fmt.Errorf("aws scan status requires region")
	case strings.TrimSpace(boundary.ServiceKind) == "":
		return fmt.Errorf("aws scan status requires service_kind")
	case strings.TrimSpace(boundary.GenerationID) == "":
		return fmt.Errorf("aws scan status requires generation_id")
	case boundary.FencingToken <= 0:
		return fmt.Errorf("aws scan status requires positive fencing token")
	default:
		return nil
	}
}

func validateAWSScanStatusMutation(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read AWS scan status mutation result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("AWS scan status mutation rejected by stale fence")
	}
	return nil
}

func (s AWSScanStatusStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
