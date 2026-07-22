// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// impactAnchorLabelDisjunction is the label set the Neo4j-compat impact reads
// seed their by-id node anchors with. It enumerates every graph label whose
// nodes carry a queryable `id` property so that an anchor of the shape
// `MATCH (n:<disjunction>) WHERE n.id = $id` resolves exactly the same node
// set as the prior unlabeled `MATCH (n) WHERE n.id = $id` while the planner
// seeds a per-label index seek instead of scanning every node in the graph.
//
// Two distinct patterns set the `id` property on nodes:
//
//  1. Labels with an `id` uniqueness constraint (and a nornicdb_*_id_lookup
//     index in internal/graph/schema.go): Repository, EvidenceArtifact,
//     Workload, WorkloadInstance, Endpoint, CloudAction, Platform.
//
//  2. Labels whose canonical writer sets `.id = row.uid` alongside the stable
//     `.uid` identity: CloudResource (cloud_resource_node_writer.go),
//     TerraformResource (config-declared, canonical_node_cypher.go's generic
//     content-entity pipeline), TerraformStateResource (state-observed,
//     tfstate_canonical_writer.go -- #5443 split this off TerraformResource;
//     it still sets `.id = row.uid`, so it must stay in this disjunction too,
//     or by-id impact reads silently stop finding state-observed resources),
//     TerraformModule (tfstate_canonical_writer.go),
//     TerraformOutput (tfstate_canonical_writer.go),
//     KubernetesWorkload (kubernetes_workload_node_writer.go).
//     For these labels the `id` value equals the `uid` value, so a caller that
//     passes the node's uid as the query parameter will resolve via `id`.
//     The prior unlabeled scan found them; the labeled disjunction must too.
//
// DataAsset is retained from the change-surface resolver anchor set for
// future-proofing; it currently sets only `uid` and will not match the `id`
// predicate, so its presence is harmless.
//
// The disjunction-with-property anchor is the shared Cypher/Bolt contract
// shape used by the canonical edge writers, so it is portable across NornicDB
// and Neo4j and does not introduce a backend branch.
const impactAnchorLabelDisjunction = "Repository|Workload|WorkloadInstance|CloudResource|TerraformResource|TerraformStateResource|TerraformModule|TerraformOutput|KubernetesWorkload|DataAsset|Platform|Endpoint|CloudAction|EvidenceArtifact"
