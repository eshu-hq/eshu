package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// DesiredCollectorInstance is the declarative source-of-truth shape reconciled
// into durable collector_instances rows.
type DesiredCollectorInstance struct {
	InstanceID    string
	CollectorKind scope.CollectorKind
	Mode          CollectorMode
	Enabled       bool
	Bootstrap     bool
	ClaimsEnabled bool
	DisplayName   string
	Configuration string
}

// Validate checks that the desired collector instance is well formed.
func (d DesiredCollectorInstance) Validate() error {
	if err := validateIdentifier("instance_id", d.InstanceID); err != nil {
		return err
	}
	if err := validateIdentifier("collector_kind", string(d.CollectorKind)); err != nil {
		return err
	}
	if err := d.Mode.Validate(); err != nil {
		return err
	}
	if err := validateJSONDocument("configuration", d.Configuration); err != nil {
		return err
	}
	if d.CollectorKind == scope.CollectorTerraformState {
		if err := ValidateTerraformStateCollectorConfiguration(d.Configuration); err != nil {
			return err
		}
	}
	return nil
}

// CollectorInstance is the durable row shape for one reconciled collector
// runtime instance.
type CollectorInstance struct {
	InstanceID     string
	CollectorKind  scope.CollectorKind
	Mode           CollectorMode
	Enabled        bool
	Bootstrap      bool
	ClaimsEnabled  bool
	DisplayName    string
	Configuration  string
	LastObservedAt time.Time
	DeactivatedAt  time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validate checks that the stored collector instance has durable identity.
func (i CollectorInstance) Validate() error {
	if err := validateIdentifier("instance_id", i.InstanceID); err != nil {
		return err
	}
	if err := validateIdentifier("collector_kind", string(i.CollectorKind)); err != nil {
		return err
	}
	if err := i.Mode.Validate(); err != nil {
		return err
	}
	if err := validateJSONDocument("configuration", i.Configuration); err != nil {
		return err
	}
	if err := validateTime("last_observed_at", i.LastObservedAt); err != nil {
		return err
	}
	if err := validateTime("created_at", i.CreatedAt); err != nil {
		return err
	}
	if err := validateTime("updated_at", i.UpdatedAt); err != nil {
		return err
	}
	if i.UpdatedAt.Before(i.CreatedAt) {
		return fmt.Errorf("updated_at must not be before created_at")
	}
	if i.LastObservedAt.Before(i.CreatedAt) {
		return fmt.Errorf("last_observed_at must not be before created_at")
	}
	if !i.DeactivatedAt.IsZero() && i.DeactivatedAt.Before(i.CreatedAt) {
		return fmt.Errorf("deactivated_at must not be before created_at")
	}
	return nil
}

// Materialize binds one desired collector instance to the supplied observation
// timestamp so it can be persisted durably.
func (d DesiredCollectorInstance) Materialize(observedAt time.Time) CollectorInstance {
	now := observedAt.UTC()
	return CollectorInstance{
		InstanceID:     strings.TrimSpace(d.InstanceID),
		CollectorKind:  d.CollectorKind,
		Mode:           d.Mode,
		Enabled:        d.Enabled,
		Bootstrap:      d.Bootstrap,
		ClaimsEnabled:  d.ClaimsEnabled,
		DisplayName:    strings.TrimSpace(d.DisplayName),
		Configuration:  normalizeJSONDocument(d.Configuration),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func normalizeJSONDocument(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "{}"
	}
	return trimmed
}

func validateJSONDocument(field, raw string) error {
	normalized := normalizeJSONDocument(raw)
	if !json.Valid([]byte(normalized)) {
		return fmt.Errorf("%s must be valid JSON", field)
	}
	return nil
}
