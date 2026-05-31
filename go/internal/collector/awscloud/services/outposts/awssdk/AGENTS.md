# AGENTS.md - internal/collector/awscloud/services/outposts/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Outposts SDK pagination, safe metadata mapping, and telemetry.
3. `exclusion_test.go` - the build-time gate that fails if an address/order/
   billing read or a mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned Outposts fact selection.
5. `../README.md` - Outposts scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Outposts SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to `List*` and `Get*` reads for
  outposts, sites, assets, and resource tags. The exclusion test fails the build
  if any method is not a List/Get read, matches a mutation name, or matches an
  address/order/billing/connection/pricing/capacity read name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only operational identity. NEVER read or persist physical site street
  addresses, ISO country codes, city/state/region, free-form notes, shipping or
  contact details, or rack physical-property logistics.
- Copy only asset id, type, rack id, compute lifecycle state, and rack-unit
  elevation from `AssetInfo`; never host id, instance families, or capacity
  inventory.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Outposts metadata read by extending `Client` and the `apiClient`
  interface with another `List*` or `Get*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any address/order/billing read or mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is operational identity
  and does not reveal a physical address, contact detail, or logistics value.

## What Not To Change Without An ADR

- Do not call `GetSiteAddress`, any order/billing/connection/catalog read, any
  pricing/renewal/capacity-task read, any instance-type read, or any Outposts
  mutation API.
- Do not infer workload, environment, deployment, or ownership truth from
  Outposts names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
