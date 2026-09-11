// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B6 root alias shim for #6642: type aliases and thin forwarders for the moved work-item evidence family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/workitem"
)

// work_item_alias.go is the root alias shim for the work-item evidence
// handler family (#6642, modelled on admin_alias.go and the sibling #6642
// language move's language_alias.go shim). WorkItemHandler and its
// method/store/decode files moved to workitem/.
// Names the rest of the program still spells `query.X` (handler wiring, cmd
// routers, staying root callers and tests) alias here so the move touches no
// caller outside the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import workitem directly.

// WorkItemHandler is the work-item evidence handler family type. Its home is
// workitem/; this alias keeps cmd/api's and cmd/mcp-server's wiring_router.go
// struct literals, internal/mcp's dispatch tests, and staying root tests
// spelling query.WorkItemHandler unchanged.
type WorkItemHandler = workitem.Handler

// WorkItemEvidenceFilter bounds direct work-item evidence reads. Its home is
// workitem/; this alias keeps internal/mcp/dispatch_work_item_authz_test.go
// and staying root tests spelling query.WorkItemEvidenceFilter unchanged.
type WorkItemEvidenceFilter = workitem.EvidenceFilter

// WorkItemEvidencePage is one bounded work-item evidence page. Its home is
// workitem/; this alias keeps internal/mcp/dispatch_work_item_authz_test.go
// and staying root tests spelling query.WorkItemEvidencePage unchanged.
type WorkItemEvidencePage = workitem.EvidencePage

// WorkItemEvidenceRow is one redacted source-fact row from a work-item
// collector. Its home is workitem/. See #6642.
type WorkItemEvidenceRow = workitem.EvidenceRow

// WorkItemEvidenceStore reads bounded Jira/work-item source facts. Its home
// is workitem/; this alias keeps internal/mcp/route_serves_data_registry*.go
// and staying root callers spelling query.WorkItemEvidenceStore unchanged.
type WorkItemEvidenceStore = workitem.EvidenceStore

// PostgresWorkItemEvidenceStore reads active work-item source facts from
// Postgres. Its home is workitem/ (implemented as workitem.PostgresEvidenceStore).
// See #6642.
type PostgresWorkItemEvidenceStore = workitem.PostgresEvidenceStore

// NewPostgresWorkItemEvidenceStore creates a Postgres-backed work-item
// evidence store. Its home is workitem/; this forwarder keeps cmd/api's and
// cmd/mcp-server's wiring calling query.NewPostgresWorkItemEvidenceStore
// unchanged. Both production call sites pass *sql.DB, so the forwarder
// narrows to that concrete type rather than re-exporting the leaf's
// unexported workItemEvidenceQueryer interface. See #6642.
func NewPostgresWorkItemEvidenceStore(db *sql.DB) PostgresWorkItemEvidenceStore {
	return workitem.NewPostgresEvidenceStore(db)
}

// workItemEvidenceCapability is the capability id this family's routes serve
// their truth envelope under. Its home is workitem/, exported there as
// EvidenceCapability because contract_work_item.go's capability-matrix
// registration is the caller that needs this unexported root spelling.
// See #6642.
const workItemEvidenceCapability = workitem.EvidenceCapability

// workItemEvidenceFactKinds bounds the work-item evidence read to the whole
// work_item fact family. Its home is workitem/, exported there as
// EvidenceFactKinds because service_story_target_support.go is the caller
// that needs this unexported root spelling. See #6642.
var workItemEvidenceFactKinds = workitem.EvidenceFactKinds

// The seven evidence-state labels keep their pre-move root spellings so any
// caller that still reads query.WorkItemEvidenceState* compiles unchanged
// (none is checked in today; the forwards honor the "root keeps every
// pre-move spelling" contract of this move). Their home is workitem/, as
// workitem.EvidenceState*. See #6642.
const (
	WorkItemEvidenceStateExactProviderFact     = workitem.EvidenceStateExactProviderFact
	WorkItemEvidenceStateUnsupportedLinkType   = workitem.EvidenceStateUnsupportedLinkType
	WorkItemEvidenceStateMissingEvidence       = workitem.EvidenceStateMissingEvidence
	WorkItemEvidenceStateStaleEvidence         = workitem.EvidenceStateStaleEvidence
	WorkItemEvidenceStatePermissionHidden      = workitem.EvidenceStatePermissionHidden
	WorkItemEvidenceStateRejectedUnsafePayload = workitem.EvidenceStateRejectedUnsafePayload
	WorkItemEvidenceStateMetadataWarning       = workitem.EvidenceStateMetadataWarning
)
