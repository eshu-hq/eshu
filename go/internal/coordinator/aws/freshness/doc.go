// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package freshness defines the normalized AWS event-driven refresh trigger
// contract.
//
// A freshness trigger is only a wake-up signal. It never marks AWS inventory
// truth fresh by itself; it coalesces provider events into the existing AWS
// collector claim shape so a normal metadata scan can refresh the affected
// account, region, and service tuple. Baseline scheduled scans remain
// authoritative and should continue to run even when this layer is enabled.
//
// The package is host-owned scheduling state (coordinator area): the trigger
// store, the freshness and scheduled planners, and webhook intake import it,
// and it resolves to no collector or satellite code (see
// TestProductionFilesHaveNoCollectorImports), so the coordinator keeps
// scheduling AWS work after the in-tree collector tree is deleted. The
// service-kind vocabulary in servicekinds.go is the schedulable set, pinned
// to the scanner registry until the registry leaves the tree.
package freshness
