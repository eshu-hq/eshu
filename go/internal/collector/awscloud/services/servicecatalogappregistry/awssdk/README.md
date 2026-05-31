# Service Catalog AppRegistry SDK Adapter

## Purpose

`internal/collector/awscloud/services/servicecatalogappregistry/awssdk` adapts
the AWS SDK for Go v2 Service Catalog AppRegistry client into the metadata-only
`servicecatalogappregistry.Client` interface. It reads application and
attribute-group metadata, an application's associated attribute groups and
associated resources, and resource tags, then maps them into scanner-owned
types.

## Ownership boundary

This package owns AWS SDK pagination, type mapping, and the API-call/throttle
telemetry wrapper. It does not own fact emission, relationship rules, or
identity selection. Those belong to the parent scanner package.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - SDK adapter implementing `servicecatalogappregistry.Client`.
- `NewClient` - builds the adapter for one claimed AWS boundary.

## Read surface (metadata-only, List-only by construction)

- `ListApplications` - application summaries.
- `ListAttributeGroups` - attribute-group summaries.
- `ListAttributeGroupsForApplication` - the attribute groups an application is
  associated with (ARNs only).
- `ListAssociatedResources` - the resources an application is associated with
  (ARN, name, AppRegistry resource type only).
- `ListTagsForResource` - resource tags.

The adapter never calls `GetAttributeGroup`, `GetConfiguration`, or
`GetAssociatedResource` (which return content bodies or tag-value detail), and
never calls any Create/Update/Delete/Associate/Disassociate/Put/Tag mutation.
A reflection guard test (`exclusion_test.go`) fails the build if a
content-read, mutation, or non-List method ever reaches the adapter interface.

## Telemetry

`recordAPICall` wraps every API call in the shared
`telemetry.SpanAWSServicePaginationPage` span and increments the shared
`eshu_dp_aws_api_calls_total` and `eshu_dp_aws_throttle_total` counters. No
bespoke instrument is added. Throttle errors are classified through the shared
smithy API-error code check and recorded; the adapter never retries internally.

## Gotchas / invariants

- Page every list API to exhaustion via `NextToken`.
- Never read the attribute-group content body or an associated-resource tag
  value; only the resource identity and type are metadata.
- Trim whitespace on every string used as an id, ARN, or tag key.

## Related docs

- `../README.md` for the AppRegistry scanner contract.
- `../../../README.md` for the shared AWS observation and envelope contract.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
