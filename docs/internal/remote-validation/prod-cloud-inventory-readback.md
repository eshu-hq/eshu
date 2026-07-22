# prod-cloud-inventory-readback — production validation

Capability: `cloud_inventory.readback.list` (tool `list_cloud_resource_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: account_project_or_subscription`, `p95_latency_ms: 5000`,
`max_truth_level: derived`.

## Claim validated

Bounded paginated readback of reducer-owned `reducer_cloud_resource_identity` rows with
provider, normalized identity, `management_origin`, evidence-layer flags, and provider-neutral
source state.

## Committed reproducible evidence

**Canonical identity listing and scoped-token safety** — `go/internal/query/cloud_inventory_readback_test.go`:
`TestCloudInventoryHandlerListsCanonicalIdentities`,
`TestCloudInventoryHandlerScopedEmptyGrantReturnsEmptyWithoutQuery`,
`TestCloudInventoryHandlerScopedGrantHitsRealStoreAndReturnsRowData`,
`TestCloudInventoryHandlerUnscopedQueryStaysUnfiltered`,
`TestCloudInventoryReadbackSelectsOnlyActiveScopeGenerations`,
`TestCloudInventoryHandlerRejectsUnknownProvider`,
`TestCloudInventoryHandlerRejectsUnknownManagementOrigin`, and
`TestCloudInventoryHandlerUnsupportedProfile`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCloudInventoryHandler -count=1
cd go && go test ./internal/query -run TestCloudInventoryReadbackSelectsOnlyActiveScopeGenerations -count=1
```

**Row-shape correctness (tag fingerprints, identity-policy evidence, attributes, freshness)** —
`go/internal/query/cloud_inventory_read_model_test.go`:
`TestCloudInventoryResourceViewSurfacesTagFingerprints`,
`TestCloudInventoryResourceViewSurfacesBoundedIdentityPolicyEvidence`,
`TestCloudInventoryResourceViewSurfacesAttributes`,
`TestCloudInventoryResourceViewDropsNestedMapFromAttributes`, and
`TestCloudInventoryResourceViewSurfacesBoundedResourceChangeFreshness`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCloudInventoryResourceView -count=1
```

**Scoped-token grant filtering (W6) performance/observability record** —
`docs/internal/evidence/5167-w6-scoped-cloud-routes.md` (#5167) documents the scoped-token grant
filtering added to cloud/ecosystem routes, including this readback family.

**Contract declaration** — `go/internal/query/openapi_cloud_inventory_test.go`:
`TestOpenAPICloudInventoryDocumentsIdentityPolicyEvidence` and
`TestOpenAPICloudInventoryDocumentsResourceChangeFreshness`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPICloudInventory -count=1
```

## Notes

No private data: fixtures use synthetic cloud resource UIDs and provider-neutral identity
fields; no real cloud account identifiers.

Related: #5552 (burn-down).
