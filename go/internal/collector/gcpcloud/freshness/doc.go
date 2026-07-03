// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package freshness defines the normalized GCP Cloud Asset Inventory (CAI)
// event-driven refresh trigger contract.
//
// A freshness trigger is only a wake-up signal. It never marks GCP inventory
// truth fresh by itself; it coalesces CAI feed notifications into the existing
// GCP collector claim shape (parent scope, asset type, location) so a normal
// metadata scan can refresh the affected slice. Baseline scheduled scans
// remain authoritative and should continue to run even when this layer is
// enabled.
package freshness
