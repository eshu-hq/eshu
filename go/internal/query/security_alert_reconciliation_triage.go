// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

// SecurityAlertMissingEvidence names a row-level reconciliation gap without
// embedding raw provider payloads or private source details.
type SecurityAlertMissingEvidence struct {
	Kind       string `json:"kind"`
	Reason     string `json:"reason"`
	EvidenceID string `json:"evidence_id,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

func securityAlertMissingEvidenceVal(payload map[string]any, key string) []SecurityAlertMissingEvidence {
	items, ok := payload[key].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]SecurityAlertMissingEvidence, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		row := SecurityAlertMissingEvidence{
			Kind:       strings.TrimSpace(fmt.Sprint(raw["kind"])),
			Reason:     strings.TrimSpace(fmt.Sprint(raw["reason"])),
			EvidenceID: strings.TrimSpace(fmt.Sprint(raw["evidence_id"])),
			Detail:     strings.TrimSpace(fmt.Sprint(raw["detail"])),
		}
		if row.Kind == "" || row.Kind == "<nil>" || row.Reason == "" || row.Reason == "<nil>" {
			continue
		}
		if row.EvidenceID == "<nil>" {
			row.EvidenceID = ""
		}
		if row.Detail == "<nil>" {
			row.Detail = ""
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
