// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shard

import "strings"

// RetirementMarker suffixes the acceptance unit id of a retirement work item.
// The coordinator plans one retirement item for each scope of an org target
// that the current partition no longer plans (a merged-away shard, or the base
// scope of a target that split); the collector answers it with an empty
// generation instead of a provider read. The marker is the field the collector
// reads to tell the two items apart: a collection item keeps
// acceptance_unit_id == scope_id. Nothing about it is specific to shards, so
// retiring a removed target (#9071) can reuse it.
const RetirementMarker = "#retire"

// RetirementFreshnessHint is the freshness hint of every retirement
// generation. The collector stamps it; the ingestion store's
// unchanged-generation check then skips a replayed retirement, and the query
// layer reads it to tell an active retirement generation, which holds no
// data, from a collection (#9019). It is durable: stored generations carry it.
const RetirementFreshnessHint = "security_alert_retired:v1"

// RetirementAcceptanceUnitID returns the acceptance unit id of the retirement
// work item for scopeID.
func RetirementAcceptanceUnitID(scopeID string) string {
	return strings.TrimSpace(scopeID) + RetirementMarker
}

// IsRetirement reports whether a work item with scopeID and acceptanceUnitID
// is a retirement item.
func IsRetirement(scopeID, acceptanceUnitID string) bool {
	scopeID = strings.TrimSpace(scopeID)
	return scopeID != "" && strings.TrimSpace(acceptanceUnitID) == scopeID+RetirementMarker
}
