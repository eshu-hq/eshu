// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replay

import "github.com/eshu-hq/eshu/go/internal/collector"

// Source is the read side of a deterministic replay flavor. Every flavor that
// replays recorded data back through the collector boundary — the cassette
// reader today, parser-fixture and other envelope flavors later — implements
// Source by yielding one collector.CollectedGeneration per recorded scope, with
// no live credentials and no network calls.
//
// Source intentionally mirrors collector.Source so a replay flavor drops into
// the same collector.Service poll loop the live collector uses; the embedded
// interface keeps the two contracts from drifting. Flavors that record at a
// different seam (for example the input tape, which records HTTP responses via
// an http.RoundTripper rather than emitting collected generations) are not
// Sources and live in their own packages under replay.
type Source interface {
	collector.Source
}
