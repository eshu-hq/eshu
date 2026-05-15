package freshness

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// EventKind identifies the normalized AWS provider signal family.
type EventKind string

const (
	// EventKindConfigChange represents an AWS Config resource change signal.
	EventKindConfigChange EventKind = "config_change"
	// EventKindCloudTrailAPI represents an EventBridge CloudTrail API signal.
	EventKindCloudTrailAPI EventKind = "cloudtrail_api"
)

// TriggerStatus describes durable AWS freshness trigger handoff state.
type TriggerStatus string

const (
	// TriggerStatusQueued means the trigger is waiting for workflow handoff.
	TriggerStatusQueued TriggerStatus = "queued"
	// TriggerStatusClaimed means a handoff actor owns the trigger row.
	TriggerStatusClaimed TriggerStatus = "claimed"
	// TriggerStatusHandedOff means workflow work items were created.
	TriggerStatusHandedOff TriggerStatus = "handed_off"
	// TriggerStatusFailed means handoff failed and needs operator attention.
	TriggerStatusFailed TriggerStatus = "failed"
)

// Trigger is one normalized AWS freshness event targeted at a scan tuple.
type Trigger struct {
	EventID      string
	Kind         EventKind
	AccountID    string
	Region       string
	ServiceKind  string
	ResourceType string
	ResourceID   string
	ObservedAt   time.Time
}

// StoredTrigger is the durable coalesced form of a Trigger.
type StoredTrigger struct {
	Trigger
	TriggerID      string
	DeliveryKey    string
	FreshnessKey   string
	Status         TriggerStatus
	DuplicateCount int
	ReceivedAt     time.Time
	UpdatedAt      time.Time
}

// Target is the existing AWS collector claim target derived from a trigger.
type Target struct {
	AccountID   string
	Region      string
	ServiceKind string
}

// Validate checks that a trigger is bounded to a supported AWS scan tuple.
func (t Trigger) Validate() error {
	t = t.normalized()
	if t.EventID == "" {
		return fmt.Errorf("aws freshness trigger requires event_id")
	}
	switch t.Kind {
	case EventKindConfigChange, EventKindCloudTrailAPI:
	default:
		return fmt.Errorf("unsupported aws freshness event kind %q", t.Kind)
	}
	if !isAWSAccountID(t.AccountID) {
		return fmt.Errorf("account_id must be a 12-digit AWS account ID")
	}
	if t.Region == "" {
		return fmt.Errorf("region is required")
	}
	if strings.Contains(t.Region, "*") {
		return fmt.Errorf("region must not contain wildcard")
	}
	if t.ServiceKind == "" {
		return fmt.Errorf("service_kind is required")
	}
	if strings.Contains(t.ServiceKind, "*") {
		return fmt.Errorf("service_kind must not contain wildcard")
	}
	if !awsruntime.SupportsServiceKind(t.ServiceKind) {
		return fmt.Errorf("unsupported service_kind %q", t.ServiceKind)
	}
	if t.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at must not be zero")
	}
	return nil
}

// Target returns the AWS collector claim target for this trigger.
func (t Trigger) Target() Target {
	t = t.normalized()
	return Target{
		AccountID:   t.AccountID,
		Region:      t.Region,
		ServiceKind: t.ServiceKind,
	}
}

// NewStoredTrigger builds durable keys for a validated freshness trigger.
func NewStoredTrigger(trigger Trigger, receivedAt time.Time) (StoredTrigger, error) {
	if receivedAt.IsZero() {
		return StoredTrigger{}, fmt.Errorf("received_at must not be zero")
	}
	trigger = trigger.normalized()
	if err := trigger.Validate(); err != nil {
		return StoredTrigger{}, err
	}
	deliveryKey := trigger.deliveryKey()
	freshnessKey := trigger.Target().FreshnessKey()
	return StoredTrigger{
		Trigger:      trigger,
		TriggerID:    facts.StableID("AWSFreshnessTrigger", map[string]any{"freshness_key": freshnessKey}),
		DeliveryKey:  deliveryKey,
		FreshnessKey: freshnessKey,
		Status:       TriggerStatusQueued,
		ReceivedAt:   receivedAt.UTC(),
		UpdatedAt:    receivedAt.UTC(),
	}, nil
}

// FreshnessKey returns the coalescing identity for targeted AWS refresh.
func (t Target) FreshnessKey() string {
	return strings.Join([]string{
		strings.TrimSpace(t.AccountID),
		strings.TrimSpace(t.Region),
		strings.TrimSpace(t.ServiceKind),
	}, ":")
}

// ScopeID returns the AWS collector scope for the target tuple.
func (t Target) ScopeID() string {
	return "aws:" + t.FreshnessKey()
}

// AcceptanceUnitID returns the JSON payload consumed by the AWS claim runtime.
func (t Target) AcceptanceUnitID() (string, error) {
	payload := struct {
		AccountID   string `json:"account_id"`
		Region      string `json:"region"`
		ServiceKind string `json:"service_kind"`
	}{
		AccountID:   strings.TrimSpace(t.AccountID),
		Region:      strings.TrimSpace(t.Region),
		ServiceKind: strings.TrimSpace(t.ServiceKind),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode AWS freshness acceptance unit: %w", err)
	}
	return string(encoded), nil
}

func (t Trigger) normalized() Trigger {
	t.EventID = strings.TrimSpace(t.EventID)
	t.Kind = EventKind(strings.TrimSpace(string(t.Kind)))
	t.AccountID = strings.TrimSpace(t.AccountID)
	t.Region = strings.TrimSpace(t.Region)
	t.ServiceKind = strings.TrimSpace(t.ServiceKind)
	t.ResourceType = strings.TrimSpace(t.ResourceType)
	t.ResourceID = strings.TrimSpace(t.ResourceID)
	t.ObservedAt = t.ObservedAt.UTC()
	return t
}

func (t Trigger) deliveryKey() string {
	return strings.Join([]string{
		string(t.Kind),
		t.EventID,
		t.AccountID,
		t.Region,
	}, ":")
}

func isAWSAccountID(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
