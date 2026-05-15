package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
)

// AWSFreshnessStore persists AWS event-driven refresh triggers for later
// workflow handoff.
type AWSFreshnessStore struct {
	db ExecQueryer
}

// NewAWSFreshnessStore constructs a Postgres-backed AWS freshness store.
func NewAWSFreshnessStore(db ExecQueryer) *AWSFreshnessStore {
	return &AWSFreshnessStore{db: db}
}

// AWSFreshnessSchemaSQL returns the DDL for the AWS freshness trigger store.
func AWSFreshnessSchemaSQL() string {
	return awsFreshnessSchemaSQL
}

// EnsureSchema applies the AWS freshness trigger schema.
func (s *AWSFreshnessStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("AWS freshness store database is required")
	}
	if _, err := s.db.ExecContext(ctx, awsFreshnessSchemaSQL); err != nil {
		return fmt.Errorf("ensure AWS freshness schema: %w", err)
	}
	return nil
}

// StoreTrigger persists and coalesces one normalized AWS freshness event.
func (s *AWSFreshnessStore) StoreTrigger(
	ctx context.Context,
	trigger freshness.Trigger,
	receivedAt time.Time,
) (freshness.StoredTrigger, error) {
	if s.db == nil {
		return freshness.StoredTrigger{}, errors.New("AWS freshness store database is required")
	}
	stored, err := freshness.NewStoredTrigger(trigger, receivedAt)
	if err != nil {
		return freshness.StoredTrigger{}, err
	}
	rows, err := s.db.QueryContext(
		ctx,
		storeAWSFreshnessTriggerQuery,
		stored.TriggerID,
		stored.DeliveryKey,
		stored.FreshnessKey,
		string(stored.Kind),
		stored.EventID,
		stored.AccountID,
		stored.Region,
		stored.ServiceKind,
		stored.ResourceType,
		stored.ResourceID,
		string(stored.Status),
		stored.ObservedAt,
		stored.ReceivedAt,
		stored.UpdatedAt,
	)
	if err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store AWS freshness trigger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return freshness.StoredTrigger{}, fmt.Errorf("store AWS freshness trigger: %w", err)
		}
		return freshness.StoredTrigger{}, errors.New("store AWS freshness trigger returned no row")
	}
	stored, err = scanAWSFreshnessTrigger(rows)
	if err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store AWS freshness trigger: %w", err)
	}
	if err := rows.Err(); err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store AWS freshness trigger: %w", err)
	}
	return stored, nil
}

// ClaimQueuedTriggers marks queued triggers as claimed for one handoff actor.
func (s *AWSFreshnessStore) ClaimQueuedTriggers(
	ctx context.Context,
	owner string,
	claimedAt time.Time,
	limit int,
) ([]freshness.StoredTrigger, error) {
	if s.db == nil {
		return nil, errors.New("AWS freshness store database is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("AWS freshness claim owner is required")
	}
	if claimedAt.IsZero() {
		return nil, errors.New("AWS freshness claimed_at is required")
	}
	if limit <= 0 {
		return nil, errors.New("AWS freshness claim limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, claimQueuedAWSFreshnessTriggersQuery, limit, owner, claimedAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("claim AWS freshness triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	triggers := make([]freshness.StoredTrigger, 0)
	for rows.Next() {
		trigger, err := scanAWSFreshnessTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("claim AWS freshness triggers: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim AWS freshness triggers: %w", err)
	}
	return triggers, nil
}

// MarkTriggersHandedOff records successful workflow handoff for claimed
// triggers.
func (s *AWSFreshnessStore) MarkTriggersHandedOff(ctx context.Context, triggerIDs []string, handedOffAt time.Time) error {
	if s.db == nil {
		return errors.New("AWS freshness store database is required")
	}
	cleaned := cleanAWSFreshnessTriggerIDs(triggerIDs)
	if len(cleaned) == 0 {
		return errors.New("AWS freshness trigger ids are required")
	}
	if handedOffAt.IsZero() {
		return errors.New("AWS freshness handed_off_at is required")
	}
	args := awsFreshnessTriggerIDArgs(cleaned, handedOffAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkAWSFreshnessTriggersHandedOffQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark AWS freshness triggers handed off: %w", err)
	}
	return nil
}

// MarkTriggersFailed records failed workflow handoff for claimed triggers.
func (s *AWSFreshnessStore) MarkTriggersFailed(
	ctx context.Context,
	triggerIDs []string,
	failedAt time.Time,
	failureClass string,
	failureMessage string,
) error {
	if s.db == nil {
		return errors.New("AWS freshness store database is required")
	}
	cleaned := cleanAWSFreshnessTriggerIDs(triggerIDs)
	if len(cleaned) == 0 {
		return errors.New("AWS freshness trigger ids are required")
	}
	if failedAt.IsZero() {
		return errors.New("AWS freshness failed_at is required")
	}
	failureClass = strings.TrimSpace(failureClass)
	if failureClass == "" {
		return errors.New("AWS freshness failure class is required")
	}
	args := awsFreshnessTriggerIDArgs(cleaned, failureClass, strings.TrimSpace(failureMessage), failedAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkAWSFreshnessTriggersFailedQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark AWS freshness triggers failed: %w", err)
	}
	return nil
}

func scanAWSFreshnessTrigger(rows Rows) (freshness.StoredTrigger, error) {
	var stored freshness.StoredTrigger
	var kind, status string
	if err := rows.Scan(
		&stored.TriggerID,
		&stored.DeliveryKey,
		&stored.FreshnessKey,
		&kind,
		&stored.EventID,
		&stored.AccountID,
		&stored.Region,
		&stored.ServiceKind,
		&stored.ResourceType,
		&stored.ResourceID,
		&status,
		&stored.DuplicateCount,
		&stored.ObservedAt,
		&stored.ReceivedAt,
		&stored.UpdatedAt,
	); err != nil {
		return freshness.StoredTrigger{}, err
	}
	stored.Kind = freshness.EventKind(kind)
	stored.Status = freshness.TriggerStatus(status)
	return stored, nil
}

func buildMarkAWSFreshnessTriggersHandedOffQuery(idCount int) string {
	timestampParam := idCount + 1
	return fmt.Sprintf(markAWSFreshnessTriggersHandedOffQueryFormat, timestampParam, timestampParam, awsFreshnessTriggerIDPlaceholders(idCount))
}

func buildMarkAWSFreshnessTriggersFailedQuery(idCount int) string {
	failureClassParam := idCount + 1
	failureMessageParam := idCount + 2
	timestampParam := idCount + 3
	return fmt.Sprintf(
		markAWSFreshnessTriggersFailedQueryFormat,
		failureClassParam,
		failureMessageParam,
		timestampParam,
		timestampParam,
		awsFreshnessTriggerIDPlaceholders(idCount),
	)
}

func cleanAWSFreshnessTriggerIDs(ids []string) []string {
	cleaned := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	return cleaned
}

func awsFreshnessTriggerIDPlaceholders(count int) string {
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return strings.Join(placeholders, ", ")
}

func awsFreshnessTriggerIDArgs(ids []string, extra ...any) []any {
	args := make([]any, 0, len(ids)+len(extra))
	for _, id := range ids {
		args = append(args, id)
	}
	return append(args, extra...)
}

func awsFreshnessBootstrapDefinition() Definition {
	return Definition{
		Name: "aws_freshness_triggers",
		Path: "schema/data-plane/postgres/020_aws_freshness_triggers.sql",
		SQL:  awsFreshnessSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, awsFreshnessBootstrapDefinition())
}
