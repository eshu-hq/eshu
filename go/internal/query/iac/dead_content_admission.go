// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// IaCDeadContentAdmissionContractVersion versions the #9669 G4
// supplemental admission-contract shape for the dead-route content
// fallback behind POST /api/v0/iac/dead. It adopts exactly the four
// ContentStore reads the dead route exercises when the
// reducer-materialized rows are absent or empty — ListRepositories
// for the repo-name enrichment, MatchRepositories for the
// access-filtered selector resolution, ListRepoFiles for the bounded
// per-repo file page, and GetFileContent for the blank-file reload —
// re-declared here in this IaC-scoped shape instead of embedding
// ContentStore, so the pin grants nothing beyond those four reads and
// no other family can cite it as coverage. A breaking change to the
// read shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const IaCDeadContentAdmissionContractVersion = "v0"

// IaCDeadContentAdmission is the versioned supplemental
// admission-contract port for the dead-route content fallback. The
// satellite keeps the HTTP shape and calls this versioned port, which
// the core-owned content store serves. Narrow by construction: the
// four methods re-declare the shared-port signatures in this
// IaC-scoped shape instead of embedding or aliasing ContentStore, so
// no other family can cite this pin as coverage.
//
// The IaCReadAdmission v0 pin stays untouched: its eight-method
// frozen set is byte-identical, and this supplement rides beside it,
// never inside it.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type IaCDeadContentAdmission interface {
	ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error)
	MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)
	ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error)
	GetFileContent(context.Context, string, string) (*querycontract.FileContent, error)
}

// The mirror assertion keeps the supplemental pin honest about its
// source: ContentStore already serves every read this pin adopts, so
// a one-sided signature drift on any of the four methods fails
// compilation here. It pins no wider store surface — the assertion
// names the shared port only as the shape this pin mirrors, and no
// other family may cite this pin as coverage. A new shared-port
// method is adopted only by deliberate re-declaration plus a frozen-
// list update.
var _ IaCDeadContentAdmission = querycontract.ContentStore(nil)
