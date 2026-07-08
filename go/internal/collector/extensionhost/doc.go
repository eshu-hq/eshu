// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package extensionhost runs out-of-tree collector SDK extensions through the
// same claim-aware collector intake used by first-party hosted collectors.
//
// Callers provide an already-claimed workflow item, a component manifest, and a
// host-owned runner; the package returns collected generations or classified
// failures for collector.ClaimedService. It validates SDK output and
// manifest-declared payload schema references before mapping facts, rejects
// returned claim identity mismatches, and keeps workflow claim mutation,
// stale-fence handling, and commits outside the extension boundary.
//
// Two runners implement the Runner contract: ProcessRunner launches a local
// process (development and first-party use), and OCIRunner launches a
// digest-pinned OCI artifact under container isolation (no network by default,
// read-only root filesystem, non-root user, all capabilities dropped, no new
// privileges, and no Eshu datastore/graph/reducer/API/MCP/workflow handles).
// Both speak the same bounded JSON SDK contract over stdin/stdout; OCIRunner
// refuses any image reference that is not digest-pinned.
package extensionhost
