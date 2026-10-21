// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the decode and fact-load/write families (schemadecode, factdecode, factwrite, factload, payloadcore)
// (issue #6061). It merges the per-family *_compat.go files listed below
// with no behavior change: every alias and forwarder is preserved
// byte-identical under its stanza marker. A family move adds a stanza
// to the matching bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md). Each entry is deleted
// once its last caller has moved; see the importer-migration child issue.
// The code-function-summary stanza below was fully retired once its last
// callers named [summary] (go/internal/reducer/code/function/summary)
// directly (#6626 tail bucket 1).
//
// Stanzas merged here:
//   - decode_seam_compat.go
//   - decode_seam_compat2.go
//   - decode_seam_compat3.go
//   - quarantine_compat.go
//   - reducer_fact_write_compat.go
//   - scoped_fact_loader_compat.go
//   - shared_payload_delta_compat.go
//   - codeowners-ownership family move (#6061; no prior compat file)
//   - cross_scope_readiness_compat.go (relocated byte-identical from
//     compat_projection.go to keep that bucket under the 500-line cap)
//   - shared-projection domains + intent row (H5 root-remnant fold, issue
//     #6061; folded from shared_projection.go)

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// Stanza: decode_seam_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the remaining root call sites keep their current spelling; each
// entry is deleted once its last caller has moved into a family subpackage.
// The four incident-routing entries were removed when their only callers
// moved into internal/reducer/incident, which imports schemadecode directly.
// The four code-function/taint/interproc entries were removed when their last
// caller (codedataflow_input_invalid_test.go) named schemadecode directly
// (#6626 batch 6 bucket 3).
// The 14 decode* aliases were retired once their last callers named [schemadecode] directly (#6626).

// This file is the transitional compatibility surface for the fact-decode and
// quarantine mechanism that moved to [factdecode] (issue #6061). Reducer-root
// call sites and the external packages that name the exported quarantine types
// keep their current spelling; each entry is deleted once its last caller has
// moved into a family subpackage.
// The 11 quarantine aliases and forwarders were retired once their last callers named [factdecode] directly (#6626).

// Stanza: reducer_fact_write_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the reducer fact
// batch writers that moved to [factwrite] (issue #6061). Each entry is deleted
// once its last reducer-root caller has moved into a family subpackage.
// The 12 fact-writer aliases and forwarders were retired once their last callers named [factwrite] directly (#6626).

// Stanza: scoped_fact_loader_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the scoped fact
// loader that moved to [factload] (issue #6061). Reducer-root call sites and the
// external packages naming FactLoader keep their current spelling; each entry is
// deleted once its last caller has moved into a family subpackage.

// FactLoader loads fact envelopes for one scope generation.
type FactLoader = factload.FactLoader

// Stanza: shared_payload_delta_compat.go (merged; do not recreate this file).
// This file holds the payload/delta forwarders that used to live in the
// semantic_entity_*.go files before the semantic_entity family moved to
// [code/semantic] (issue #6061). Each one already forwarded to a
// shared-tier package; they stay in root because other root families that
// have not moved out yet still call them by their unqualified root spelling.
// code/semantic calls the shared-tier functions directly instead of
// reaching back into root for these.

// deltaScopeRepositorySet, uniqueRepositoryIDs, applyRepoRefreshDeltaScope,
// and sharedProjectionRowRepoID were retired once their last callers named
// [sharedintent.DeltaScopeRepositorySet], [sharedintent.UniqueRepositoryIDs],
// [sharedintent.ApplyRepoRefreshDeltaScope], and [sharedintent.RowRepoID]
// directly (#6626 tail bucket 2).
// The 5 payload/delta forwarders were retired once their last callers named [payloadcore] directly (#6626).

// Stanza: decode_seam_compat2.go (merged; do not recreate this file).

// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the 3 remaining root call sites keep their current spelling;
// each entry is deleted once its last caller has moved into a family
// subpackage. The 17 decodeObservability* entries were removed when their only
// callers moved into internal/reducer/obscoverage, and the three Vault entries
// when theirs moved into internal/reducer/secretsiam; both subpackages import
// schemadecode directly. The 9 vulnerability/scanner/package-consumption
// entries were removed with the supplychain/core move: their only callers
// were supply-chain files that now import schemadecode directly (#6061).
// The 3 remaining aliases were retired once their last callers named [schemadecode] directly (#6626).

// Stanza: decode_seam_compat3.go (merged; do not recreate this file).

// decodeCodeDataflowFunction and decodeCodeDataflowScanned were retired once
// their last caller (codedataflow_input_invalid_test.go) named schemadecode
// directly (#6626 batch 6 bucket 3). The factschemaEnvelope forwarder was
// removed earlier with the supplychain/core move: its only callers were
// supply-chain files that now import schemadecode directly (#6061).

// Stanza: codeowners-ownership family move (#6061; no prior compat file).
// The codeowners.ownership fact extraction, materialization, and delta-scope
// family moved to [owners] (go/internal/reducer/code/owners). Every entry
// keeps the reducer.X spelling for the additive-domain registry's handler
// wiring and the reducer root's factload_materialization_bench_test.go
// corpus-coverage guard. Each entry is deleted once its last caller names
// [owners] directly.
// The 3 codeowners entries were retired once their last callers named [owners] directly (#6626).

// Stanza: cross_scope_readiness_compat.go (merged; do not recreate this
// file). Relocated byte-identical from compat_projection.go (issue #6061
// code/ subtree move) to keep that bucket under the 500-line cap.
// The cross-scope producer-readiness floor and dependency catalog moved to
// [crossscope] (issue #6061). The readiness entries were retired once their
// last callers named [crossscope] directly (#6626 tail bucket 2);
// CrossScopeDependency (a [reducercontract] alias, not a crossscope entry)
// stays until its own callers move.
// CrossScopeDependency (a [reducercontract] alias, not a crossscope entry)
// was retired once its own callers named [reducercontract] directly (#6626).

// CrossScopeCompletionEdges, crossScopeDependenciesForRegistration,
// CrossScopeProducerNotReadyFailureClass, crossScopeProducerNotReadyError,
// CrossScopeProducerReadiness, and CrossScopeProducerReadinessByDomain were
// retired once their last callers named [crossscope.CompletionEdges],
// [crossscope.DependenciesForRegistration],
// [crossscope.ProducerNotReadyFailureClass],
// [crossscope.ProducerNotReadyError], [crossscope.ProducerReadiness], and
// [crossscope.ProducerReadinessByDomain] directly (#6626 tail bucket 2).

// Stanza: shared-projection domains + intent row (H5 root-remnant fold,
// issue #6061). HandlesRoute/RunsIn ride the ordering-safe shared-projection
// path (#2721, #2722); InvokesCloudAction/CodeownersOwnershipEdges/
// SubmodulePinEdges MERGE both endpoints inline, needing no readiness gate.
const (
	DomainRepoDependency           = reducercontract.DomainRepoDependency
	DomainWorkloadDependency       = reducercontract.DomainWorkloadDependency
	DomainCodeCalls                = reducercontract.DomainCodeCalls
	DomainSQLRelationships         = reducercontract.DomainSQLRelationships
	DomainShellExec                = reducercontract.DomainShellExec
	DomainInheritanceEdges         = reducercontract.DomainInheritanceEdges
	DomainDocumentationEdges       = reducercontract.DomainDocumentationEdges
	DomainRationaleEdges           = reducercontract.DomainRationaleEdges
	DomainDeployableUnitEdges      = reducercontract.DomainDeployableUnitEdges
	DomainHandlesRoute             = reducercontract.DomainHandlesRoute
	DomainRunsIn                   = reducercontract.DomainRunsIn
	DomainInvokesCloudAction       = reducercontract.DomainInvokesCloudAction
	DomainCodeownersOwnershipEdges = reducercontract.DomainCodeownersOwnershipEdges
	DomainSubmodulePinEdges        = reducercontract.DomainSubmodulePinEdges
)

// allProjectionDomains forwards to [reducercontract.ProjectionDomains].
var allProjectionDomains = reducercontract.ProjectionDomains()
