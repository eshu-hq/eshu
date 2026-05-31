package workflow

import (
	"fmt"
	"strings"
)

const (
	derivedTargetPlanningModeRotating   = "rotating"
	derivedTargetPlanningModeSinglePass = "single_pass"
)

func validateDerivedTargetPlanningMode(raw string) error {
	switch strings.TrimSpace(raw) {
	case "", derivedTargetPlanningModeRotating, derivedTargetPlanningModeSinglePass:
		return nil
	default:
		return fmt.Errorf(`must be "rotating" or "single_pass"`)
	}
}
