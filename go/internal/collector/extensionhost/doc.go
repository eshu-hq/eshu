// Package extensionhost runs out-of-tree collector SDK extensions through the
// same claim-aware collector intake used by first-party hosted collectors.
//
// Callers provide an already-claimed workflow item, a component manifest, and a
// host-owned runner; the package returns collected generations or classified
// failures for collector.ClaimedService. It validates SDK output before mapping
// facts, rejects returned claim identity mismatches, and keeps workflow claim
// mutation, stale-fence handling, and commits outside the extension boundary.
package extensionhost
