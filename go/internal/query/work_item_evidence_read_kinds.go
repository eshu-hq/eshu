// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/facts"

// workItemEvidenceFactKinds bounds the evidence read to the whole work_item
// family the fact-kind registry maps to GET /api/v0/work-items/evidence. It is
// exactly facts.WorkItemFactKinds() — the single source of truth for the
// family — so the SQL kind list, the decode switch in decodeWorkItemEvidenceRow,
// and the registry stay in lockstep and a future family addition trips the
// drift guard (TestWorkItemEvidenceFactKindsMatchRegistrySet) instead of
// silently narrowing the read.
//
// work_item.metadata_warning is included: #4887 gave WorkItemEvidenceRow the
// warning's own contract fields (metadata_type, warning_reason,
// provider_id_fingerprint) and a distinct metadata_warning evidence state, so a
// metadata-collection warning now surfaces as itself instead of an ordinary
// provider fact. WorkItemFactKinds returns a fresh clone, so assigning it here
// does not share a backing array with the registry.
var workItemEvidenceFactKinds = facts.WorkItemFactKinds()
