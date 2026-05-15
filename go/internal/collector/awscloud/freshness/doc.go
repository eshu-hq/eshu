// Package freshness defines the normalized AWS event-driven refresh trigger
// contract.
//
// A freshness trigger is only a wake-up signal. It never marks AWS inventory
// truth fresh by itself; it coalesces provider events into the existing AWS
// collector claim shape so a normal metadata scan can refresh the affected
// account, region, and service tuple. Baseline scheduled scans remain
// authoritative and should continue to run even when this layer is enabled.
package freshness
