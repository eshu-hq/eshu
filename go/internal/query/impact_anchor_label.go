package query

// impactAnchorLabelDisjunction is the label set the Neo4j-compat impact reads
// seed their by-id node anchors with. It enumerates the id-bearing platform
// graph labels that a canonical entity id can resolve to (the deployment,
// infrastructure, and repository nodes that carry a plain `id` property and an
// id uniqueness constraint or lookup index in the graph schema), so an anchor of
// the shape `MATCH (n:<disjunction>) WHERE n.id = $id` resolves the same node
// set as the prior unlabeled `MATCH (n) WHERE n.id = $id` while the planner seeds
// a per-label index seek instead of scanning every node in the graph.
//
// The prior unlabeled anchors gave the Neo4j planner no label to seed from, so
// the by-id predicate forced an all-node scan on Neo4j-compat (issue #3567). The
// label list mirrors the impact-anchor set the change-surface resolver already
// uses (Repository, Workload, WorkloadInstance, CloudResource, TerraformModule,
// DataAsset) plus the additional id-constrained deployment-evidence labels the
// graph schema declares. Labels whose identity key is `uid` rather than `id`
// (e.g. TerraformModule, DataAsset) stay in the set so the disjunction matches
// the same node set as before; they simply never satisfy the `id` predicate.
//
// The disjunction-with-property anchor is the shared Cypher/Bolt contract shape
// used by the canonical edge writers, so it is portable across NornicDB and
// Neo4j and does not introduce a backend branch.
const impactAnchorLabelDisjunction = "Repository|Workload|WorkloadInstance|CloudResource|TerraformModule|DataAsset|Platform|Endpoint|CloudAction|EvidenceArtifact"
