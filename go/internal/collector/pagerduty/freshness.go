// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"encoding/json"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// freshnessHint returns the stable content digest for one PagerDuty
// collection (#8149): the sorted keyed payload digests of the emitted
// envelopes. Each entry covers its fact kind, stable key, and full payload,
// so same-key payload drift — incident status, urgency, priority,
// assignments, teams, service and escalation references, and updated or
// resolved timestamps, plus lifecycle, change-event, config, and warning
// fields — mints a new generation instead of skipping as unchanged. It
// covers what the collector actually emits and never the raw provider body,
// which can carry out-of-window content that would mint a new generation
// while the emitted facts stand still. Envelope transport metadata — fact
// and generation IDs, fencing token, and the collection-clock observed_at —
// stays out, so identical PagerDuty state at a later tick shares the hint
// and the store skips the duplicate instead of minting per tick. Every entry
// is JSON-encoded before sorting, so the hint is insensitive to envelope
// emission order while nested maps stay canonical through StableID's JSON
// encoding.
func freshnessHint(envs []facts.Envelope) string {
	encoded := make([]string, 0, len(envs))
	for _, env := range envs {
		entry := map[string]any{
			"fact_kind":  env.FactKind,
			"payload":    env.Payload,
			"stable_key": env.StableFactKey,
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return ""
		}
		encoded = append(encoded, string(raw))
	}
	sort.Strings(encoded)
	return facts.StableID("pagerduty.collection.freshness", map[string]any{"envelopes": encoded})
}
