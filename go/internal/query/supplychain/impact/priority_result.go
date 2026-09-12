// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

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
		reasonCode := querycontract.StringVal(row, "reason_code")
		if reasonCode == "" {
			continue
		}
		out = append(out, SupplyChainImpactPriorityContribution{
			ReasonCode:   reasonCode,
			Input:        querycontract.StringVal(row, "input"),
			Value:        querycontract.StringVal(row, "value"),
			Contribution: int(querycontract.FloatVal(row, "contribution")),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
