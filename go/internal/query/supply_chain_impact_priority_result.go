// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func decodeSupplyChainImpactPriorityContributions(raw any) []SupplyChainImpactPriorityContribution {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]SupplyChainImpactPriorityContribution, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		reasonCode := StringVal(row, "reason_code")
		if reasonCode == "" {
			continue
		}
		out = append(out, SupplyChainImpactPriorityContribution{
			ReasonCode:   reasonCode,
			Input:        StringVal(row, "input"),
			Value:        StringVal(row, "value"),
			Contribution: int(floatVal(row, "contribution")),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
