// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
)

func parseSelfTestTriggers(
	registryPath string,
	gateID string,
	declared *[]string,
	primary map[string]struct{},
) ([]string, error) {
	if declared == nil {
		return nil, nil
	}
	if len(*declared) == 0 {
		return nil, fmt.Errorf("ci-gates registry %s: gate %q has empty self_test_triggers (omit the field for fail-closed always-run behavior)", registryPath, gateID)
	}
	result := make([]string, 0, len(*declared))
	seen := make(map[string]struct{}, len(*declared))
	for _, rawTrigger := range *declared {
		trigger := strings.TrimSpace(rawTrigger)
		if trigger == "" {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has blank self_test_triggers entry", registryPath, gateID)
		}
		if _, duplicate := seen[trigger]; duplicate {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has duplicate self_test_trigger %q", registryPath, gateID, trigger)
		}
		if _, covered := primary[trigger]; !covered {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q self_test_trigger %q must also appear in triggers", registryPath, gateID, trigger)
		}
		seen[trigger] = struct{}{}
		result = append(result, trigger)
	}
	return result, nil
}
