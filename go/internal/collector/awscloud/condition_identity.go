// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import "strings"

// addConditionSummaryIdentity extends conditioned policy-statement identities
// without changing the existing identity for unconditional statements.
func addConditionSummaryIdentity(identity map[string]any, conditionKeys, conditionOperators []string) {
	if len(conditionKeys) == 0 && len(conditionOperators) == 0 {
		return
	}
	identity["condition_keys"] = strings.Join(conditionKeys, ",")
	identity["condition_operators"] = strings.Join(conditionOperators, ",")
}
