// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageruntime

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// packageFreshnessHint returns the stable content digest for one package
// metadata collection (#8003): the sorted stable keys of the emitted
// envelopes. It covers what the collector actually emits — in-window package,
// version, artifact, hint, event, hosting, and warning facts — and never the
// raw document body, which can carry out-of-window content that would mint a
// new generation while the emitted facts stand still.
func packageFreshnessHint(envelopes []facts.Envelope) string {
	keys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		keys = append(keys, envelope.StableFactKey)
	}
	sort.Strings(keys)
	return facts.StableID("package-registry.collection.freshness", map[string]any{"stable_fact_keys": keys})
}
