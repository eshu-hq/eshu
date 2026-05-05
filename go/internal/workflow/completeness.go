package workflow

import (
	"fmt"
	"strings"
)

// Validate checks that the completeness checkpoint is durably identifiable.
func (c CompletenessState) Validate() error {
	if err := validateIdentifier("run_id", c.RunID); err != nil {
		return err
	}
	if err := validateIdentifier("collector_kind", string(c.CollectorKind)); err != nil {
		return err
	}
	if err := validateIdentifier("keyspace", string(c.Keyspace)); err != nil {
		return err
	}
	if err := validateIdentifier("phase_name", c.PhaseName); err != nil {
		return err
	}
	if strings.TrimSpace(c.Status) == "" {
		return fmt.Errorf("status must not be blank")
	}
	if err := validateTime("observed_at", c.ObservedAt); err != nil {
		return err
	}
	if err := validateTime("updated_at", c.UpdatedAt); err != nil {
		return err
	}
	if c.UpdatedAt.Before(c.ObservedAt) {
		return fmt.Errorf("updated_at must not be before observed_at")
	}
	return nil
}
