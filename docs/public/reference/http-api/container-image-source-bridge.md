# Container Image Source Bridge

Use `GET /api/v0/supply-chain/container-images/identities` when a caller needs
container image identity evidence for a source repository without treating an
OCI image repository as source repository truth.

## Source Repository Scope

The list, count, and inventory routes accept `source_repository_id` as the
source-repository bridge selector:

- `GET /api/v0/supply-chain/container-images/identities`
- `GET /api/v0/supply-chain/container-images/identities/count`
- `GET /api/v0/supply-chain/container-images/identities/inventory`

`source_repository_id` is resolved as a source repository selector through the
existing repository resolver when needed, then used to read reducer-owned
`source_repository_ids` anchors. The existing `repository_id` parameter is still
an OCI/image repository identity such as
`oci-registry://registry.example/team/api`; it is not source-repository truth.
Callers may combine the two parameters to ask for source bridge evidence inside
one OCI repository, but the API keeps both fields separate in the response.
`source_repository_ids` may come from explicit deployment or CI image evidence,
or from OCI image config provenance labels such as
`org.opencontainers.image.source` only when the label resolves to exactly one
known source repository selector. Image names, registry paths, and tags do not
create source anchors by themselves.

List responses include `source_bridge` when `source_repository_id` is present.
The bridge summary returns the source repository id, image repository ids, and
explicit `missing_evidence` such as:

- `deployment_image_reference_missing`
- `image_registry_observation_missing`
- `source_to_image_correlation_missing`

`warnings=["ambiguous_image_repository"]` means the source repository has image
identity rows across more than one OCI repository, so callers should narrow the
question before treating one image repository as selected.

This list route is one of the seven gated supply-chain list routes, so its
responses also carry the `collector_readiness` envelope
(`collector_kind=oci_registry`) that distinguishes an unconfigured feeding
collector from a configured-but-empty page. See
[Gated List Collector Readiness](evidence-and-supply-chain.md#gated-list-collector-readiness).

## Performance Evidence

No-Regression Evidence: `go test ./internal/query -run
'Test(SupplyChainListContainerImageIdentities|ContainerImageSourceBridgeMissingEvidenceMatrix|ContainerImageIdentityQueryUses|OpenAPISpecIncludesContainerImageSourceRepositoryBridge|ContainerImageIdentityAggregate)'
-count=1` and `go test ./internal/mcp -run
'Test(ResolveRouteMapsContainerImageIdentitiesToBoundedQuery|ContainerImageIdentityToolSchemaAdvertisesSourceRepositoryScope|ResolveRouteMapsContainerImageAggregatesForwardSourceRepositoryScope|ContainerImageAggregateToolSchemasAdvertiseSourceRepositoryScope)'
-count=1` cover API list/count/inventory source scoping, row response fields,
missing-hop classification, OpenAPI schema exposure, and MCP route/schema
parity.
No-Regression Evidence: `go test ./internal/reducer -run
'TestBuildContainerImageIdentityDecisions(UsesOCIConfigSourceLabel|RejectsMissingConflictingAndMalformedOCIConfigSourceLabels)|TestSBOMAttachmentInheritsRepositoryAnchorFromLabelProvenImageIdentity'
-count=1` proves OCI config source/revision labels can create source repository
anchors only through exact known-repository URL matches, while missing,
conflicting, malformed, or ambiguous labels stay out of image identity rows.
The same test proves SBOM attachment decisions inherit repository anchors
through the reducer image identity path when the subject digest matches.

No-Observability-Change: source-repository bridge reads reuse the existing
`query.container_image_identities` and
`query.container_image_identity_aggregate` spans, HTTP/MCP envelopes, response
`count`, `limit`, `truncated`, and `next_cursor` or `next_offset` fields, and
Postgres fact-read instrumentation. The query shape is bounded by
`fact_kind='reducer_container_image_identity'`, active generation predicates,
the GIN-indexed `payload->'source_repository_ids' ? $n` predicate, caller
`limit+1` pagination for list/inventory, and deterministic fact-id or bucket
ordering. After selector resolution, the identity read performs no whole-graph
fanout, no reducer work, no queue work, and no runtime configuration change.
