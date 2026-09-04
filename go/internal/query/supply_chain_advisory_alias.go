// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/advisory"
)

// This file preserves the root package query surface cmd/api and
// cmd/mcp-server still use for the advisory read models. The implementation
// moved to internal/query/supplychain/advisory (#6060 lane A); these aliases
// forward unchanged so the wiring call sites compile without touching other
// lanes. The hub PR3 owns the final alias surface for this family (handler
// aliases join here when the handlers move); keep this file to the
// constructor-level compatibility cmd/* needs until then.

// PostgresAdvisoryCatalogStore reads a bounded, browsable page of canonical
// vulnerability advisories. See advisory.PostgresAdvisoryCatalogStore.
type PostgresAdvisoryCatalogStore = advisory.PostgresAdvisoryCatalogStore

// PostgresAdvisoryEvidenceStore reads active vulnerability source facts and
// groups them into canonical advisory evidence rows. See
// advisory.PostgresAdvisoryEvidenceStore.
type PostgresAdvisoryEvidenceStore = advisory.PostgresAdvisoryEvidenceStore

// NewPostgresAdvisoryCatalogStore constructs the Postgres-backed catalog
// read model. Forwards unchanged to
// advisory.NewPostgresAdvisoryCatalogStore.
func NewPostgresAdvisoryCatalogStore(db advisory.AdvisoryEvidenceQueryer) PostgresAdvisoryCatalogStore {
	return advisory.NewPostgresAdvisoryCatalogStore(db)
}

// NewPostgresAdvisoryEvidenceStore constructs the Postgres-backed advisory
// evidence read model. Forwards unchanged to
// advisory.NewPostgresAdvisoryEvidenceStore.
func NewPostgresAdvisoryEvidenceStore(db advisory.AdvisoryEvidenceQueryer) PostgresAdvisoryEvidenceStore {
	return advisory.NewPostgresAdvisoryEvidenceStore(db)
}
