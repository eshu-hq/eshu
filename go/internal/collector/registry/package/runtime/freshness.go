// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"encoding/json"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// packageFreshnessHint returns the stable content digest for one package
// metadata collection (#8003): the sorted keyed payload digests of the
// emitted envelopes. Each entry covers its fact kind, stable key, and full
// payload, so same-key payload drift — yanked, deprecated, retracted,
// unlisted, checksum, URL, published_at, visibility, and warning-text changes
// — mints a new generation instead of skipping as unchanged (#8003 F2). It
// covers what the collector actually emits — in-window package, version,
// artifact, hint, event, hosting, and warning facts — and never the raw
// document body, which can carry out-of-window content that would mint a new
// generation while the emitted facts stand still. Every entry is JSON-encoded
// before sorting, so the hint is insensitive to parser emission order while
// nested maps stay canonical through StableID's JSON encoding.
func packageFreshnessHint(envelopes []facts.Envelope) string {
	encoded := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		entry := map[string]any{
			"fact_kind":  envelope.FactKind,
			"payload":    envelope.Payload,
			"stable_key": envelope.StableFactKey,
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return ""
		}
		encoded = append(encoded, string(raw))
	}
	sort.Strings(encoded)
	return facts.StableID("package-registry.collection.freshness", map[string]any{"envelopes": encoded})
}
