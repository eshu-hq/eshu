# Separate SSM host observations from vulnerability findings (#9302)

Status: Proposed; no implementation or deployment is approved. Part of #9295.

**Propose distinct host application and platform observations, with no
vulnerability finding until a separately proven host applicability contract
exists.** This provisional choice is not accepted until the measurement and
decisions below are complete.

The representative account volume measurement is incomplete. A QA managed-node
discovery read succeeded after the owner updated IAM; application volume and
fleet coverage remain unmeasured. The decision, implementation issues, and live QA
claims remain open until that measurement and arbiter acceptance are recorded.
#9301's AMI integration is also a prerequisite.

## Context

Systems Manager inventory can describe an EC2 managed node's installed
applications and platform. The existing AWS SSM scanner covers Parameter Store;
it does not collect this host inventory. Host packages are evidence about an
instance, not evidence that an image digest contains those packages.

The existing OS-package model derives an image subject from a sibling
`scanner_worker.analysis` in the same scope and generation; it does not turn
a scope identifier into an image digest. The impact handler has an advisory-
target loading stage for OS-package evidence; see
`go/internal/reducer/supplychain/core/evidence_load.go` and
`go/internal/reducer/supplychainmodel/types.go`. These are image-scanner
contracts, not proof that a host inventory can use them. An OS-package finding
can have an empty image digest when the sibling is absent; it must not use the
scope as a substitute. An SSM inventory item alone is not a vulnerability finding.

## Options

| Option | Cost and disposition |
| --- | --- |
| Emit distinct application and platform observations | Proposed: preserves source meaning but requires new contracts and a separate host-impact design. |
| Reuse `vulnerability.os_package` | Rejected provisionally: its image-scan semantics and matcher do not establish host applicability. |
| Inventory only, without host impact | Possible first phase; does not complete the requested host-impact decision. |

## Decision

The satellite `eshu-hq/eshu-collector-aws-cloud` would collect bounded,
allowlisted SSM inventory into typed host platform and application-observation
facts. Package facts require a separately validated subset. Exact kind names,
payloads, keys, schema versions, and lifecycle remain undecided. The provisional ruling selects distinct application and platform
observation kinds; SSM application rows must not be emitted as
`vulnerability.os_package`. New graph-truth kinds need typed contracts and
registry bindings, reducer and conformance work in `eshu`, not just a collector
manifest. Changing the meaning or stable key of an existing kind is a major
contract change with a conversion path; optional additive fields are minor.

An EC2 host subject must include the AWS partition, account, region and
managed node instance identifier. Require EC2 resource type and corroborating
EC2 identity in that boundary; an identifier prefix alone is insufficient.
For the first EC2-host design, exclude non-EC2 and conflicting identities from
EC2/AMI impact with visible counts. A separate managed-node host model may be
considered later; it must not inherit an EC2 or AMI join. The AMI link comes
from the instance-to-AMI evidence delivered by #9301. Environment and service attribution require their own corroborating
relationships; neither names nor AMI membership alone prove them. Define
collision, deletion, generation, retry, and cross-account handling before
selecting stable keys.

The existing OS-package matcher consumes vulnerability-scan observations and
requires a supported package manager, distro version, architecture, vendor
repository class and advisory source, plus a matching package identity. It does
not establish vulnerability by comparing installed versions to advisory ranges.
`AWS:Application` includes architecture and a provider-specific package ID,
but does not guarantee the package manager, distro/version, vendor repository
and advisory provenance, PURL, or advisory applicability. It can include
applications other than OS packages. Do not invent a PURL or vendor
classification from a name. Inventory-only facts may land as a phase, but they
do not complete the requested host impact design. A supported platform-specific subset needs retained source
evidence and a distinct host vulnerability applicability contract.
Unknown mappings remain unresolved. See `go/internal/reducer/supplychain/core/match.go`
and the [AWS inventory schema](https://docs.aws.amazon.com/systems-manager/latest/userguide/inventory-schema.html).

A host-aware correlation path would admit package evidence only for a uniquely
identified, sufficiently fresh host, then connect it to a vulnerability through
a separately designed and proven advisory applicability and platform-specific
version comparison, with source provenance. The existing image-scan matcher
cannot provide this conclusion from normalized application inventory alone. It
must not invent an image digest, infer a running service from an installed package, or treat an AMI
as proof that the running host still has its original contents.

| Case | Required behavior to prove |
| --- | --- |
| Positive | Matching account, region, instance, fresh inventory, and supported package/version evidence yield the intended host impact. |
| Negative | A different account, region, instance or package, an unmatched version, and an AMI alone do not yield host impact. |
| Ambiguous | Conflicting identity, multiple plausible matches, unknown version semantics, stale data, or partial inventory remain unresolved; do not choose a winner by ordering. |

The implementation proof plan must specify tests comparing fixture intent,
admitted reducer facts, direct graph nodes and edges, and API/MCP output.
Run those tests after decision acceptance and implementation; their live results
are not a prerequisite for accepting this design. Replays, duplicate delivery, generation
supersession, incomplete pages, and concurrent projection require explicit
proof. Operator signals must distinguish denied, partial, stale, and successful
inventory without exposing package values in logs.

## Collection, privacy, and freshness questions

The proposed minimum is `AWS:Application` observations and the platform fields
needed for host identity and potential version matching. An application is not
a package until platform-specific semantics are established. Custom inventory,
file and network entries,
registry contents, arbitrary command output, and raw unselected attributes are
excluded. Exclude URL, Summary, host names and IP addresses unless a later
reviewed contract establishes a specific need. The exact allowlist and any platform-
specific package identifier and version rules need a privacy and accuracy review.
Do not persist collected values merely because the API returns them.

[`ListInventoryEntries`](https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_ListInventoryEntries.html)
is per managed node and inventory type, returns at most 50 entries per page, and
provides the inventory capture time. The
[Inventory schema](https://docs.aws.amazon.com/systems-manager/latest/userguide/inventory-schema.html)
contains application fields beyond a minimal package identity. Record the
inventory capture time as source freshness; a node's last agent ping
is not the inventory capture time. Define numerical stale-age and clock-skew
limits, behavior for absent, malformed or future capture time, stale display,
and partial scan semantics before implementation. A complete snapshot needs
all discovery and selected-type pages, valid
nonrepeating pagination tokens, and consistent instance ID, type, schema and
capture time within each inventory stream before superseding prior inventory.
AWS does not promise equal capture times across types: define an allowed skew
for platform and application evidence when both come from inventory types. If
platform comes from `DescribeInstanceInformation`, it has no inventory capture
time; define a separate evidence and freshness rule rather than inventing one.
Missing pages, denial, inconsistent
capture times or incoherent type pairs mean partial or unknown, not an empty
host or a deletion. A missing inventory association or disabled collection must
also not be interpreted as an empty application set.

[`DescribeInstanceInformation`](https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_DescribeInstanceInformation.html)
omits stopped and terminated nodes. Compare it with
[`GetInventory`](https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_GetInventory.html),
which includes such node statuses, before choosing discovery and measurement
coverage. A stopped or terminated inventory record informs coverage, not current
running-host exposure. Report EC2 fleet, SSM-managed, active, and inventory-enabled counts
separately; none is automatically the denominator for the others.

The candidate measurement calls require `ssm:DescribeInstanceInformation`
and `ssm:ListInventoryEntries`. The [Systems Manager authorization reference](https://docs.aws.amazon.com/service-authorization/latest/reference/list_ssm.html)
lists no resource-level type for either action; their policy therefore needs
`Resource: "*"`. Confirm any applicable conditions and the final action set
against the accepted collection design. Do not grant patch-compliance actions
until patch collection is in the accepted scope. The source, account, region, and enabled inventory types must be explicit
in deployment configuration.

## Measurement requirements

**NOT_PROVEN:** instances × packages × scans per day; application rows are not
necessarily packages. Bytes, page count, runtime, and effect on ingestion, reducer work, storage, and retention are also unknown.
Measure a representative account with a bounded, read-only shim after access is
available. Record the exact command, account selection method without private
identifiers, UTC time, instance count, application-entry distribution, page
count, capture ages, enumeration and per-node/type page counts, bytes, source rows and content
hashes where available, and exit status. Record SSM capture cadence separately
from Eshu polling cadence. Changed-versus-unchanged rate needs two comparable
reads; otherwise mark it and any future fact-rate projection NOT_PROVEN. Report
validated OS-package counts separately only if the classification is proven;
otherwise the package count and package-rate projection stay NOT_PROVEN. State
sample limitations. Distinguish observed counts from projections. A denial is not a zero inventory result.

<details>
<summary>Evidence and measurements</summary>

### Partial QA measurement, 2026-10-28

The aggregate-only command `python3 /tmp/ssm_9302_volume_measure.py` ran
twice from 22:13:08 to 22:13:33 UTC; both invocations exited 2.
`DescribeInstanceInformation` returned 22 unique managed EC2 nodes, all
Online, in one page (0.073 seconds on the first run and 0.095 seconds on
the second). The first `GetInventory` request failed validation because it
used two result attributes. The corrected request failed with an AWS
`ThrottlingException` after 5.248 seconds; the measurement stopped.
The corrected-attempt script and aggregate summary are retained in the local
goal ledger; the first script revision and raw outputs are not retained there.
This is not a deployed or reproducible benchmark artifact. The second run
repeated discovery; it is not an independent control. No application
sample was read. Stopped-node coverage, inventory-enabled population,
application and package distributions, capture ages, bytes, and volume
projections remain NOT_PROVEN. The 22 managed online nodes are not the
entire EC2 fleet denominator.

### Fresh online-node application sample

With owner approval, `python3 /tmp/ssm_9302_application_sample.py` ran
once on 2026-10-28 from 22:29:09 to 22:29:11 UTC and exited 0. A new,
complete discovery page returned 22 unique online managed EC2 nodes, all
of which were sampled using one `AWS:Application` inventory page each.
Every response contained zero entries and no `CaptureTime`. This does
not distinguish an unconfigured inventory association from a completed
empty inventory and is not evidence of zero installed packages. The 22
responses totaled 9,658 bytes in compact JSON serialization; this is not
wire traffic or object heap size. The aggregate-only script is retained in the local goal
ledger. No `GetInventory` call or SDK retry was made in this sample.

The fresh frame has the same count as the earlier discovery, but exact
node identity equality cannot be verified because the earlier frame was
not retained. Stopped or terminated coverage, the inventory-enabled
denominator, validated package counts, capture cadence, change rate,
and daily fact volume remain NOT_PROVEN. A meaningful representative
application-volume decision needs evidence from hosts with configured
application inventory; this zero-entry sample does not meet that gate.

</details>

## Implementation plan

After acceptance, implement typed contracts and registry in `eshu`, collection
in `eshu-hq/eshu-collector-aws-cloud`, and reducer/read paths in `eshu`. Keep
the source allowlist, partial-state diagnostics and proof plan above; do not
reuse image vulnerability findings or infer missing facts as deletion. Split
implementation into issues under #9295 only after the decision is accepted.

## Consequences

**Good**

- Host observations retain their source meaning and do not manufacture image
  or vulnerability evidence.
- Explicit partial and stale states preserve prior evidence on incomplete reads.

**Bad**

- New contracts, correlation and retention work are required before host impact
  can be reported.
- Representative volume, supported version comparators and retained-data cleanup
  remain unknown; they may change the final design or operational cost.

## Acceptance criteria

- [ ] Measure a representative captured population with the denominators,
  counts, pages, bytes, timing, sampling limits and sizing bounds above.
- [ ] Settle kind names, schemas, stable keys, identity, IAM, privacy, freshness,
  partial-generation and host applicability rules.
- [ ] Specify executable positive, negative and ambiguous fixture, concurrency,
  performance, graph, API and MCP checks with expected outcomes.
- [ ] Obtain a final arbiter verdict on this completed decision before building.

## Decisions for the arbiter

1. Which exact versioned kind names, keys and schemas represent application
   observations and platform evidence?
2. Which supported host package and advisory comparator rules establish
   vulnerability applicability, and which cases remain unknown?
3. What measured age, clock-skew, cross-type-skew and bounded resource limits
   govern a complete snapshot?
4. Which discovery source and approved population provide representative
   capture and fleet coverage, and what is the retention policy?

## Obtaining representative capture evidence

First consider a read-only review of existing associations selected by the
`AWS-GatherSoftwareInventory` document name, their application-collection
settings, targets and execution status. Do not rely on an instance-only filter,
which misses tag and resource-group targets. Association status alone does not
prove an application capture: independently verify coherent dated inventory
responses and choose a representative captured population for volume sizing. This may
require separate association read permissions; it does not require retrying
`GetInventory`. Filter and aggregate locally so association targets and
inventory content do not enter logs or public issue text.

If no suitable existing capture exists, a temporary QA-only association is
a separate environment change, not an accepted step in this draft. Before
any such change, obtain owner and arbiter authorization, identify exact QA
targets, check agent and instance-role readiness and existing associations,
and explicitly enable application collection while disabling other types.
Avoid broad targets, custom/file/network/registry inventory and resource
data sync. Delete the temporary association afterward to stop collection by that
association; other associations may still collect. Do not claim built-in
inventory data was deleted: its
retention and cleanup remain unresolved. AWS documents association
targeting and overlapping-association risks in the
[inventory setup guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/inventory-collection.html)
and [troubleshooting guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/syman-inventory-troubleshooting.html).
Host overhead, retained data, request counts and any indirect costs need
measurement; do not promise zero cost.

Before accepting this record, resolve the fact-kind and host identity design,
impact matching and stale semantics, IAM and privacy scope, and measured volume.
An arbiter must accept the completed record before collector, contract, reducer,
or query implementation. File and work the accepted implementation issues in
this goal under #9295. This draft is not that acceptance.
