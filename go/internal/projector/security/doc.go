// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package security builds reducer intents for security-alert reconciliation and
// AWS security-group node and reachability-edge materialization.
//
// Callers pass an immutable intent.FactLookup that root projector assembly owns
// for one already-validated scope generation. Root rejects stale or mismatched
// fact boundaries before constructing the lookup and retains ownership of
// dispatcher order, deterministic final sorting, queue writes, retries, and
// telemetry.
//
// Each builder emits at most one intent and anchors it to the earliest matching
// fact. Source-ref identity takes precedence over the collector fallback. Alert
// reconciliation accepts only provider-alert and package-registry-package facts;
// the three security-group builders accept only AWS security-group-rule facts
// and share the aws_resource_materialization:<scope> readiness key. A missing
// accepted kind emits no intent. Payload admission remains reducer-owned, so a
// matching kind with malformed payload still schedules the existing downstream
// validation and retry path.
package security
