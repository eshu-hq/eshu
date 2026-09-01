// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

import (
	"strings"
	"time"
)

// Now resolves the timestamp a fact writer stamps on its rows, normalized to
// UTC. A nil clock means "use the wall clock", which is what production passes;
// tests inject a fixed clock so a row's observed_at is assertable.
//
// The UTC conversion is the load-bearing part. Rows written by different
// collectors are ordered against each other by observed_at, so a writer that
// stored a local time would order wrongly against every other writer while
// still type-checking and still looking correct in a single-writer test.
func Now(now func() time.Time) time.Time {
	if now != nil {
		return now().UTC()
	}

	return time.Now().UTC()
}

// CollectorKind normalizes the collector-kind column written on a fact row: it
// trims the source system and substitutes "unknown" when nothing is left.
//
// The literal "unknown" matters. Operators group fact rows by collector kind,
// and an empty string would create a second, invisible bucket alongside it.
func CollectorKind(sourceSystem string) string {
	collectorKind := strings.TrimSpace(sourceSystem)
	if collectorKind == "" {
		return "unknown"
	}

	return collectorKind
}
