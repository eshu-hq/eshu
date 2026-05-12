package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// WebhookTriggerStore persists provider webhook intake decisions for later
// targeted repository refresh handoff.
type WebhookTriggerStore struct {
	db ExecQueryer
}

// NewWebhookTriggerStore constructs a Postgres-backed webhook trigger store.
func NewWebhookTriggerStore(db ExecQueryer) *WebhookTriggerStore {
	return &WebhookTriggerStore{db: db}
}

// WebhookTriggerSchemaSQL returns the DDL for the webhook trigger store.
func WebhookTriggerSchemaSQL() string {
	return webhookTriggerSchemaSQL
}

// EnsureSchema applies the webhook trigger schema.
func (s *WebhookTriggerStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("webhook trigger store database is required")
	}
	if _, err := s.db.ExecContext(ctx, webhookTriggerSchemaSQL); err != nil {
		return fmt.Errorf("ensure webhook trigger schema: %w", err)
	}
	return nil
}

// StoreTrigger persists one normalized trigger and deduplicates by refresh
// identity. Webhook payloads remain trigger evidence only; this method does not
// mark graph or repository truth fresh.
func (s *WebhookTriggerStore) StoreTrigger(
	ctx context.Context,
	trigger webhook.Trigger,
	receivedAt time.Time,
) (webhook.StoredTrigger, error) {
	if s.db == nil {
		return webhook.StoredTrigger{}, errors.New("webhook trigger store database is required")
	}
	stored, err := prepareStoredTrigger(trigger, receivedAt)
	if err != nil {
		return webhook.StoredTrigger{}, err
	}
	rows, err := s.db.QueryContext(
		ctx,
		storeWebhookTriggerQuery,
		stored.TriggerID,
		stored.DeliveryKey,
		stored.RefreshKey,
		string(stored.Provider),
		string(stored.EventKind),
		string(stored.Decision),
		string(stored.Reason),
		stored.DeliveryID,
		stored.RepositoryExternalID,
		stored.RepositoryFullName,
		stored.DefaultBranch,
		stored.Ref,
		stored.BeforeSHA,
		stored.TargetSHA,
		stored.Action,
		stored.Sender,
		string(stored.Status),
		stored.ReceivedAt,
		stored.UpdatedAt,
	)
	if err != nil {
		return webhook.StoredTrigger{}, fmt.Errorf("store webhook trigger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return webhook.StoredTrigger{}, fmt.Errorf("store webhook trigger: %w", err)
		}
		return webhook.StoredTrigger{}, errors.New("store webhook trigger returned no row")
	}
	stored, err = scanStoredWebhookTrigger(rows)
	if err != nil {
		return webhook.StoredTrigger{}, fmt.Errorf("store webhook trigger: %w", err)
	}
	if err := rows.Err(); err != nil {
		return webhook.StoredTrigger{}, fmt.Errorf("store webhook trigger: %w", err)
	}
	return stored, nil
}

// ClaimQueuedTriggers marks queued triggers as claimed for a compatibility
// handoff actor and returns the claimed rows.
func (s *WebhookTriggerStore) ClaimQueuedTriggers(
	ctx context.Context,
	owner string,
	claimedAt time.Time,
	limit int,
) ([]webhook.StoredTrigger, error) {
	if s.db == nil {
		return nil, errors.New("webhook trigger store database is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("webhook trigger claim owner is required")
	}
	if limit <= 0 {
		return nil, errors.New("webhook trigger claim limit must be positive")
	}
	if claimedAt.IsZero() {
		return nil, errors.New("webhook trigger claimed_at is required")
	}

	rows, err := s.db.QueryContext(ctx, claimQueuedWebhookTriggersQuery, limit, owner, claimedAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("claim webhook triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	triggers := make([]webhook.StoredTrigger, 0)
	for rows.Next() {
		trigger, err := scanStoredWebhookTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("claim webhook triggers: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim webhook triggers: %w", err)
	}
	return triggers, nil
}

// MarkTriggersHandedOff records that claimed triggers were handed to the
// repository refresh selector.
func (s *WebhookTriggerStore) MarkTriggersHandedOff(ctx context.Context, triggerIDs []string, handedOffAt time.Time) error {
	if s.db == nil {
		return errors.New("webhook trigger store database is required")
	}
	cleaned := cleanTriggerIDs(triggerIDs)
	if len(cleaned) == 0 {
		return errors.New("webhook trigger ids are required")
	}
	if handedOffAt.IsZero() {
		return errors.New("webhook trigger handed_off_at is required")
	}
	args := triggerIDArgs(cleaned, handedOffAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkWebhookTriggersHandedOffQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark webhook triggers handed off: %w", err)
	}
	return nil
}

// MarkTriggersFailed records a failed compatibility handoff so claimed
// triggers do not stay invisible to operators.
func (s *WebhookTriggerStore) MarkTriggersFailed(
	ctx context.Context,
	triggerIDs []string,
	failedAt time.Time,
	failureClass string,
	failureMessage string,
) error {
	if s.db == nil {
		return errors.New("webhook trigger store database is required")
	}
	cleaned := cleanTriggerIDs(triggerIDs)
	if len(cleaned) == 0 {
		return errors.New("webhook trigger ids are required")
	}
	if failedAt.IsZero() {
		return errors.New("webhook trigger failed_at is required")
	}
	failureClass = strings.TrimSpace(failureClass)
	if failureClass == "" {
		return errors.New("webhook trigger failure class is required")
	}
	args := triggerIDArgs(cleaned, failureClass, strings.TrimSpace(failureMessage), failedAt.UTC())
	if _, err := s.db.ExecContext(
		ctx,
		buildMarkWebhookTriggersFailedQuery(len(cleaned)),
		args...,
	); err != nil {
		return fmt.Errorf("mark webhook triggers failed: %w", err)
	}
	return nil
}

func prepareStoredTrigger(trigger webhook.Trigger, receivedAt time.Time) (webhook.StoredTrigger, error) {
	if receivedAt.IsZero() {
		return webhook.StoredTrigger{}, errors.New("webhook trigger received_at is required")
	}
	deliveryKey := webhookDeliveryKey(trigger)
	refreshKey := webhookRefreshKey(trigger)
	if deliveryKey == "" {
		return webhook.StoredTrigger{}, errors.New("webhook trigger delivery key is required")
	}
	if refreshKey == "" {
		return webhook.StoredTrigger{}, errors.New("webhook trigger refresh key is required")
	}
	status := webhook.TriggerStatusQueued
	if trigger.Decision == webhook.DecisionIgnored {
		status = webhook.TriggerStatusIgnored
	}
	return webhook.StoredTrigger{
		Trigger:     trigger,
		TriggerID:   facts.StableID("WebhookRefreshTrigger", map[string]any{"refresh_key": refreshKey}),
		DeliveryKey: deliveryKey,
		RefreshKey:  refreshKey,
		Status:      status,
		ReceivedAt:  receivedAt.UTC(),
		UpdatedAt:   receivedAt.UTC(),
	}, nil
}

func webhookDeliveryKey(trigger webhook.Trigger) string {
	parts := []string{
		string(trigger.Provider),
		strings.TrimSpace(trigger.DeliveryID),
		strings.TrimSpace(trigger.RepositoryExternalID),
	}
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}
	return strings.Join(parts, ":")
}

func webhookRefreshKey(trigger webhook.Trigger) string {
	parts := []string{
		string(trigger.Provider),
		strings.TrimSpace(trigger.RepositoryExternalID),
		strings.TrimSpace(trigger.DefaultBranch),
		strings.TrimSpace(trigger.TargetSHA),
	}
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}
	return strings.Join(parts, ":")
}

func scanStoredWebhookTrigger(rows Rows) (webhook.StoredTrigger, error) {
	var stored webhook.StoredTrigger
	var provider, eventKind, decision, reason, status string
	if err := rows.Scan(
		&stored.TriggerID,
		&stored.DeliveryKey,
		&stored.RefreshKey,
		&provider,
		&eventKind,
		&decision,
		&reason,
		&stored.DeliveryID,
		&stored.RepositoryExternalID,
		&stored.RepositoryFullName,
		&stored.DefaultBranch,
		&stored.Ref,
		&stored.BeforeSHA,
		&stored.TargetSHA,
		&stored.Action,
		&stored.Sender,
		&status,
		&stored.DuplicateCount,
		&stored.ReceivedAt,
		&stored.UpdatedAt,
	); err != nil {
		return webhook.StoredTrigger{}, err
	}
	stored.Provider = webhook.Provider(provider)
	stored.EventKind = webhook.EventKind(eventKind)
	stored.Decision = webhook.Decision(decision)
	stored.Reason = webhook.DecisionReason(reason)
	stored.Status = webhook.TriggerStatus(status)
	return stored, nil
}

func cleanTriggerIDs(ids []string) []string {
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

func buildMarkWebhookTriggersHandedOffQuery(idCount int) string {
	timestampParam := idCount + 1
	return fmt.Sprintf(
		markWebhookTriggersHandedOffQueryFormat,
		timestampParam,
		timestampParam,
		triggerIDPlaceholders(idCount),
	)
}

func buildMarkWebhookTriggersFailedQuery(idCount int) string {
	failureClassParam := idCount + 1
	failureMessageParam := idCount + 2
	timestampParam := idCount + 3
	return fmt.Sprintf(
		markWebhookTriggersFailedQueryFormat,
		failureClassParam,
		failureMessageParam,
		timestampParam,
		triggerIDPlaceholders(idCount),
	)
}

func triggerIDPlaceholders(count int) string {
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return strings.Join(placeholders, ", ")
}

func triggerIDArgs(ids []string, extra ...any) []any {
	args := make([]any, 0, len(ids)+len(extra))
	for _, id := range ids {
		args = append(args, id)
	}
	return append(args, extra...)
}

func webhookTriggerBootstrapDefinition() Definition {
	return Definition{
		Name: "webhook_refresh_triggers",
		Path: "schema/data-plane/postgres/017_webhook_refresh_triggers.sql",
		SQL:  webhookTriggerSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, webhookTriggerBootstrapDefinition())
}
