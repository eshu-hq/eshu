// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package plannercontract

import (
	"fmt"
	"strings"
)

// ValidateSafePlanKey validates the coordinator's shared scheduler plan-key
// grammar. Validation ignores surrounding whitespace but does not normalize or
// return the key; callers retain ownership of the original value.
func ValidateSafePlanKey(owner string, planKey string) error {
	planKey = strings.TrimSpace(planKey)
	if planKey == "" {
		return fmt.Errorf("%s plan_key must not be blank", owner)
	}
	if strings.ContainsAny(planKey, `/\`) {
		return fmt.Errorf("%s plan_key must not include raw source locator material", owner)
	}
	for _, char := range planKey {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '.', '_', '-':
			continue
		default:
			return fmt.Errorf("%s plan_key contains unsupported character %q", owner, char)
		}
	}
	return nil
}
