// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

// SourceUIDsFromRowsByKey extracts distinct string values stored under key
// from a batch of edge rows, skipping rows where the key is absent or not a
// string. It is the shared helper for every CloudResource-edge-family handler
// (AWS, Azure, GCP relationship rows key their source uid as "source_uid";
// observability coverage rows key it as "observability_uid") so each handler
// records the same source-uid set into the ledger that it wrote to the graph.
func SourceUIDsFromRowsByKey(rows []map[string]any, key string) []string {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if uid, ok := row[key].(string); ok && uid != "" {
			seen[uid] = struct{}{}
		}
	}
	uids := make([]string, 0, len(seen))
	for uid := range seen {
		uids = append(uids, uid)
	}
	return uids
}
