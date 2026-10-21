// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the correlation families (cicdrun, crossrepo, supplychain)
// (issue #6061). It merges the per-family *_compat.go files listed below
// with no behavior change: every alias and forwarder is preserved
// byte-identical under its stanza marker. A family move adds a stanza
// to the matching bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md). Each entry is deleted
// once its last caller has moved; see the importer-migration child issue.
// The service_catalog_correlation and sbom_attestation_attachment stanzas
// below were fully retired once their last callers named [servicecatalog]
// and [sbomattest] directly (#6626 tail bucket 1). The ci_cd_run,
// cross_repo, supply_chain_impact + supply_chain_suppression, and
// shared-projection intent row + unroutable-write stanzas were fully
// retired once their last callers named [cicdrun], [crossrepo],
// [supplychaincore], [sharedintent], and [worker] directly (#6626 tail
// bucket 2).
//
// Stanzas merged here:
//   - graph-projection phase repair (H5 root-remnant fold, issue #6061;
//     folded from graph_projection_phase_repair.go and
//     graph_projection_phase_repair_runner.go)

// This bucket currently carries no live entries: every stanza below is a
// retired ledger. Each entry was deleted once its last caller moved; see the
// importer-migration child issue.

// Stanza ci_cd_run_correlation_compat.go: fully retired (#6626 tail bucket 2).
// Every caller names [cicdrun] directly; the notes below are ledger.

// Stanza cross_repo_compat.go: fully retired (#6626 tail bucket 2).
// Every caller names [crossrepo] directly; the notes below are ledger.

// Stanza sbom_attestation_attachment_compat.go: fully retired (#6626 tail bucket 1).
// Every caller names [sbomattest] directly; the notes below are ledger.

// MaxSBOMAttachmentComponentEvidenceRows, ComponentEvidence,
// ComponentEvidenceLess, and ComponentEvidenceTupleEqual were retired once
// their last callers moved to [sbomattest] (#6626 batch 2).

// sbomAttestationAttachmentFactKind is not aliased in root any more: the moved
// sbomattest family names [reducercontract.SBOMAttestationAttachmentFactKind]
// directly, and the intent.go alias this comment used to point at was removed
// once the last in-root caller moved (#6061). containerImageIdentityFactKind in
// intent.go keeps the same direct-from-contract shape (#6431).

// Stanza supply_chain_impact + supply_chain_suppression: fully retired
// (#6626 tail bucket 2). Every caller names [supplychaincore] directly;
// the notes below are ledger.

// Stanza shared-projection intent row and unroutable-write: fully retired
// (#6626 tail bucket 2). SharedProjectionIntentRow was retired when its
// last callers named sharedintent.Row directly (#6626 batch 6 bucket 5);
// the remaining input/key/row/report aliases and the Build/CarriesNoEdge
// forwarders were retired once their last callers named [sharedintent]
// and [worker] directly. The notes below are ledger.

// Stanza: graph-projection phase repair (H5 root-remnant fold, issue #6061;
// folded from graph_projection_phase_repair.go and
// graph_projection_phase_repair_runner.go): fully retired once every caller
// named [gpphase] and [repair] directly (#6626 gpphase micro-batch).
// GraphProjectionPhaseRepair, GraphProjectionPhaseRepairQueue,
// GraphProjectionPhaseRepairerConfig, GraphProjectionPhaseRepairer, and
// GraphProjectionPhaseRepairsFromStates were deleted with their last callers;
// the notes above are ledger.

// repoWideRetractRefreshPartitionKey was retired once its last callers
// named [sharedintent.RepoWideRetractRefreshPartitionKey] directly (#6626
// tail bucket 2).
