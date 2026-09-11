// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
)

// missingEvidenceVal decodes the row-level triage detail behind a
// reconciliation's missing_evidence payload field.
func missingEvidenceVal(payload map[string]any, key string) []supplychain.SecurityAlertMissingEvidence {
	items, ok := payload[key].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]supplychain.SecurityAlertMissingEvidence, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		row := supplychain.SecurityAlertMissingEvidence{
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
