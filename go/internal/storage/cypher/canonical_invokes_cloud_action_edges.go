// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// BuildInvokesCloudActionRowMap converts an invokes_cloud_action intent payload
// into the flat UNWIND parameter map for the INVOKES_CLOUD_ACTION upsert. It
// skips the row (ok=false) when any MATCH/MERGE key — function_id, action, or
// action_id — is empty so an unresolvable or actionless edge is never written.
// Provenance fields are passed through from the reducer, which derives them from
// the import-proven SDK binding.
func BuildInvokesCloudActionRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	functionID := PayloadString(payload, "function_id")
	// The cloud action is carried under "cloud_action" (not "action"), so it does
	// not collide with the shared-projection upsert/refresh discriminator that
	// sharedintent.FilterUpsertRows reads from payload["action"]. The Cypher param stays
	// "action" because it sets the CloudAction node + edge action property.
	action := PayloadString(payload, "cloud_action")
	actionID := PayloadString(payload, "action_id")
	if functionID == "" || action == "" || actionID == "" {
		return "", nil, false
	}
	return node.BatchCanonicalInvokesCloudActionUpsertCypher, map[string]any{
		"function_id":       functionID,
		"action":            action,
		"action_id":         actionID,
		"resolution_method": PayloadString(payload, "resolution_method"),
		"confidence":        PayloadFloat(payload, "confidence"),
		"evidence_source":   evidenceSource,
	}, true
}
