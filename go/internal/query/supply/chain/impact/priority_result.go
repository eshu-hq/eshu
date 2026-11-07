// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

func decodeSupplyChainImpactPriorityContributions(raw any) []packet.PriorityContribution {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]packet.PriorityContribution, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		reasonCode := querycontract.StringVal(row, "reason_code")
		if reasonCode == "" {
			continue
		}
		out = append(out, packet.PriorityContribution{
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
