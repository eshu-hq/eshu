// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func scanJSONPayload(rows *sql.Rows) (map[string]any, error) {
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if len(raw) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode payload JSON: %w", err)
	}
	return payload, nil
}

func ensureEvidencePacketURL(finding map[string]any) {
	if stringFromMap(finding, "evidence_packet_url") != "" {
		return
	}
	findingID := stringFromMap(finding, "finding_id")
	if findingID == "" {
		return
	}
	finding["evidence_packet_url"] = "/api/v0/documentation/findings/" + findingID + "/evidence-packet"
}

func documentationPermissionReason(packet map[string]any) string {
	if reason := nestedString(packet, "permissions", "denied_reason"); reason != "" {
		return reason
	}
	if reason := nestedString(packet, "states", "permission_reason"); reason != "" {
		return reason
	}
	if reason := documentationVisibilityDecision(packet).reason; reason != "" {
		return reason
	}
	return "caller cannot view documentation evidence"
}

type documentationVisibility struct {
	allowed bool
	reason  string
}

func documentationVisibilityDecision(payload map[string]any) documentationVisibility {
	if strings.EqualFold(nestedString(payload, "states", "permission_decision"), "denied") {
		return documentationVisibility{reason: "caller cannot view documentation evidence"}
	}
	if evaluated, ok := nestedBoolValue(payload, "permissions", "source_acl_evaluated"); ok && !evaluated {
		return documentationVisibility{reason: "documentation source ACL was not evaluated"}
	}
	canRead, ok := nestedBoolValue(payload, "permissions", "viewer_can_read_source")
	if !ok {
		return documentationVisibility{reason: "documentation evidence visibility is unknown"}
	}
	if !canRead {
		return documentationVisibility{reason: "caller cannot view documentation evidence"}
	}
	return documentationVisibility{allowed: true}
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func nestedString(values map[string]any, objectKey, valueKey string) string {
	nested, _ := values[objectKey].(map[string]any)
	value, _ := nested[valueKey].(string)
	return strings.TrimSpace(value)
}

func nestedBoolValue(values map[string]any, objectKey, valueKey string) (bool, bool) {
	nested, _ := values[objectKey].(map[string]any)
	value, ok := nested[valueKey].(bool)
	return value, ok
}
