// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// freshnessHint returns the stable content digest for one Jira collection
// (#7926): the collection window bounds plus the sorted digests of the fact
// envelopes the collection actually emitted, the applied issue cap, whether
// the issue search truncated past that cap, and the completeness flags.
//
// Each entry covers its fact kind, stable key, and full payload, so
// same-key content drift — an issue status flip, a new changelog transition,
// a new remote link, or drifted metadata — mints a new generation instead of
// skipping as unchanged. It covers what the collector emits and never the
// raw provider body. Envelope transport metadata — fact and generation IDs,
// fencing token, and the collection-clock observed_at — stays out, so a
// byte-identical retry shares the hint and the store skips the duplicate
// instead of minting per attempt.
//
// The window bounds are folded in, not just the content: a later window is a
// different observation even when the provider returns the same issues, so
// distinct windows store while byte-identical retries skip. The issue cap
// and truncation marker join for the same reason: the same issues under a
// wider limit, or capped versus complete, are different observations.
func freshnessHint(window CollectionWindow, result CollectionResult, envs []facts.Envelope) string {
	encoded := make([]string, 0, len(envs))
	for _, env := range envs {
		entry := map[string]any{
			"fact_kind":  env.FactKind,
			"payload":    env.Payload,
			"stable_key": env.StableFactKey,
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			// Fail open on purpose: a blank hint disables duplicate
			// detection, so the generation always stores instead of
			// risking a skipped write. That is the safe direction —
			// an extra stored generation, never lost evidence — at
			// the cost of one missed dedup. There is no logger on
			// this pure path, so the choice stays a comment rather
			// than a log line or metric.
			return ""
		}
		encoded = append(encoded, string(raw))
	}
	sort.Strings(encoded)
	return facts.StableID("jira.collection.freshness", map[string]any{
		"envelopes":         encoded,
		"issue_limit":       result.IssueLimit,
		"metadata_warnings": len(result.MetadataWarnings),
		"partial_failures":  result.Stats.PartialFailures,
		"search_truncated":  result.SearchTruncated,
		"since":             window.Since.UTC().Format(time.RFC3339Nano),
		"stale_windows":     result.Stats.StaleWindows,
		"until":             window.Until.UTC().Format(time.RFC3339Nano),
	})
}
