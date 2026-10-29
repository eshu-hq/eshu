# Separate SSM host observations from vulnerability findings (#9302)

Status: Accepted for a bounded observation-only first phase (arbiter ACCEPT
2026-10-29 on the measured numbers; this record reconciled to that ruling
after the verdict — see "Arbiter acceptance" below). Part of #9295.

2026-10-29: all four pre-acceptance blockers cleared. (a) Owner granted a
scoped create/delete association grant and ran the trigger; the temporary
QA-only association path ran and was deleted. (b) Captured-cohort volume
measured on 2 nodes (see "Captured-cohort measurement" below). (c) #9301
closed completed. (d) Arbiter verdict ACCEPT obtained. Remaining work splits
into implementation issues under #9295 — typed contracts in `eshu`,
bounded collection in `eshu-collector-aws-cloud`, reducer/read paths in
`eshu` — not inside the closed design PR #9452, which reopens only as the
accepted record.

**Propose distinct host application and platform observations, with no
vulnerability finding until a separately proven host applicability contract
exists.** Accepted for the bounded observation-only first phase: new v1.0.0
kinds `aws_ssm_host_application` and `aws_ssm_host_platform` per the keys and
allowlist below. No `vulnerability.os_package`, no `supply_chain_impact`, no
invented PURL or image digest. Host vulnerability correlation stays with
#9209. Cohort scope stays online EC2 managed nodes with confirmed capture
only; no fleet, Windows, stopped-node, or non-EC2 claim is accepted.

The representative captured-cohort volume measurement is complete (2 Linux
nodes, 2026-10-29, see below). Fleet coverage, changed-vs-unchanged rate,
ingestion/reducer/storage/retention cost, host-agent overhead, validated
OS-package counts, and AWS-side dollar cost remain NOT_PROVEN and need a
second comparable capture before any change-rate, dedup-saving, or
fleet-scale claim.
#9301's AMI integration closed completed as a prerequisite.

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
| Emit distinct application and platform observations | Proposed: preserves source meaning and supports inventory-first delivery; host impact remains a separate correlation contract. |
| Reuse `vulnerability.os_package` | Rejected provisionally: its image-scan semantics and matcher do not establish host applicability. |
| Inventory only, without host impact | Chosen first phase; host admission is specified here, while advisory correlation belongs to #9209. |

## Decision

The satellite `eshu-hq/eshu-collector-aws-cloud` would collect bounded,
allowlisted SSM inventory into two proposed new v1.0.0 fact kinds:
`aws_ssm_host_application` and `aws_ssm_host_platform`. These are observations,
not package findings. `AWS:Application` rows must not become
`vulnerability.os_package` or `supply_chain_impact`. #9209 owns advisory feeds
and correlation; a new advisory engine is not needed to deliver this first
inventory slice. New graph-truth kinds need typed contracts, registry bindings,
reducer and conformance work in `eshu`, not just a collector manifest. Adding
new kinds is a new v1 contract, not a change to an existing kind. Changing an
existing kind's meaning or stable key would instead be a major change with a
conversion path. Verified 2026-10-29 against `specs/fact-kind-registry.v1.yaml`:
no `aws_ssm`, `host_application`, or `host_platform` kind exists, so the
proposal renames and redefines nothing; at implementation the
`factschema-diff`, `payload-usage-manifest`, `fact-kind-registry`, and
`contract-source-of-truth` gates apply to the new declarations, schemas, typed
structs, and conformance fixtures.

| Proposed fact | Required payload | Stable key and lifecycle |
| --- | --- | --- |
| `aws_ssm_host_application` v1.0.0 | Partition, account, region, EC2 instance ID, source type and schema version, capture time, and the allowlisted source fields `Name`, `Version`, `Release`, `Epoch`, `PackageId`, `Architecture` when present. | Host tuple + capture time + SHA-256 of the canonical allowlisted row. Identical rows in a capture deduplicate; another capture is a new observation. A complete newer generation supersedes prior observations for that host and type. |
| `aws_ssm_host_platform` v1.0.0 | The same host tuple, `PlatformType`, `PlatformName`, `PlatformVersion` when present, source type, and observation or capture time with its provenance. | Host tuple + source type. A complete newer generation updates the platform observation; `DescribeInstanceInformation` observation time must not be labeled inventory capture time. |

The exact JSON field spelling and key encoding are finalized with the typed
schema implementation after the measured record is accepted. A missing or
invalid capture time emits no application fact and cannot supersede the last
complete snapshot. Empty application rows count as an empty snapshot only with
a coherent dated capture and complete pagination; an undated zero-row response
is unknown. No raw source row, URL, Summary, hostname or IP is stored. Hashing
the canonical selected row is for identity, not a claim that the row is an OS
package or that its contents are secret.

An EC2 host subject must include the AWS partition, account, region and
managed node instance identifier. Require EC2 resource type and corroborating
EC2 identity in that boundary; an identifier prefix alone is insufficient.
For the first EC2-host design, exclude non-EC2 and conflicting identities from
EC2/AMI impact with visible counts. A separate managed-node host model may be
considered later; it must not inherit an EC2 or AMI join. The AMI link comes
from the instance-to-AMI evidence delivered by #9301. Environment and service
attribution require their own corroborating relationships; neither names nor
AMI membership alone prove them. Reject conflicting host tuples and cross-account
or cross-region joins. Replay of the same complete capture is idempotent; a
partial or denied read preserves the prior generation with visible status.

The existing OS-package matcher consumes vulnerability-scan observations and
requires a supported package manager, distro version, architecture, vendor
repository class and advisory source, plus a matching package identity. It does
not establish vulnerability by comparing installed versions to advisory ranges.
`AWS:Application` includes architecture and a provider-specific package ID,
but does not guarantee the package manager, distro/version, vendor repository
and advisory provenance, PURL, or advisory applicability. It can include
applications other than OS packages. Do not invent a PURL or vendor
classification from a name. Inventory-only facts are the first phase. Host vulnerability applicability
belongs to #9209 and needs a separately proven platform-specific contract.
Unknown mappings remain unresolved. See `go/internal/reducer/supplychain/core/match.go`
and the [AWS inventory schema](https://docs.aws.amazon.com/systems-manager/latest/userguide/inventory-schema.html).

A future host-aware correlation path would admit package evidence only for a
uniquely identified, sufficiently fresh host, then connect it to a vulnerability
through #9209's separately proven advisory and version comparison. The existing image-scan matcher
cannot provide this conclusion from normalized application inventory alone. It
must not invent an image digest, infer a running service from an installed package, or treat an AMI
as proof that the running host still has its original contents.

| Case | Required behavior to prove |
| --- | --- |
| Positive | A matching partition, account, region and instance with a fresh complete capture admits host observations; only a separately proven comparator may later yield host impact. |
| Negative | A different host tuple, an AMI alone, or an installed application without applicable advisory evidence yields no host impact. |
| Ambiguous | Conflicting identity, unknown version semantics, stale capture or partial inventory remain unresolved; do not choose a winner by ordering. |

The implementation proof plan must specify tests comparing fixture intent,
admitted reducer facts, direct graph nodes and edges, and API/MCP output.
Run those tests after decision acceptance and implementation; their live results
are not a prerequisite for accepting this design. Replays, duplicate delivery, generation
supersession, incomplete pages, and concurrent projection require explicit
proof. Operator signals must distinguish denied, partial, stale, and successful
inventory without exposing package values in logs.

## Collection, privacy, and freshness rules

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
omits stopped and terminated nodes. The initial cohort therefore covers online EC2 managed nodes with confirmed
application capture only. It makes no stopped-node or full-fleet coverage claim.
Report target, SSM-managed, captured, and excluded counts separately; none is
automatically the denominator for the others. `GetInventory` and a full-fleet
census are unnecessary for this bounded cohort.

The candidate measurement calls require `ssm:DescribeInstanceInformation`
and `ssm:ListInventoryEntries`. The [Systems Manager authorization reference](https://docs.aws.amazon.com/service-authorization/latest/reference/list_ssm.html)
lists no resource-level type for either action; their policy therefore needs
`Resource: "*"`. Confirm any applicable conditions and the final action set
against the accepted collection design. Do not grant patch-compliance actions
until patch collection is in the accepted scope. The association-review path
additionally needs an association describe or list grant: a bounded
`ListAssociations` filtered on `AWS-GatherSoftwareInventory` was denied for
the QA measurement identity on 2026-10-29 (see evidence below). The source, account, region, and enabled inventory types must be explicit
in deployment configuration.

## Measurement requirements

**PARTIALLY PROVEN (cohort of 2, 2026-10-29):** per-host entries, pages, and
response bytes are measured (see "Captured-cohort measurement" below).
**Still NOT_PROVEN:** instances × packages × scans per day at any wider
scope; application rows are not necessarily packages. Effect on ingestion,
reducer work, storage, and retention is unknown, as are changed-vs-unchanged
rate, dedup savings, host-agent overhead, and dollar cost. A second
comparable capture is required before any of those claims.
The cohort was measured with a bounded association path after owner and
arbiter authorization. Record the exact command, selection method without private
identifiers, UTC time, target and captured counts, platform and package-population
limits, application-entry distribution, page
count, capture ages, enumeration and per-node/type page counts, bytes, source rows and content
hashes where available, and exit status. Record SSM capture cadence separately
from Eshu polling cadence. Use a conservative full-snapshot-per-poll bound: target hosts × maximum accepted
entries per host × polls per day, with requests bounded by hosts × allowed pages
per type per poll. Set positive numeric host, page, entry, age, skew, polling and
retention limits from the representative measurement before final acceptance;
missing limits fail configuration. Changed-versus-unchanged rate needs two
comparable reads; otherwise mark it and any deduplication saving NOT_PROVEN. Report
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

### Second online-node application frame (Frame C), 2026-10-29

With the standing read-only QA measurement, `python3 /tmp/ssm_9302_frame_c.py`
ran once on 2026-10-29 from 10:17:11 to 10:17:22 UTC and exited 0. The exact
command and its JSON output are recorded here; the script is a throwaway
`/tmp` probe, not a committed artifact. No `GetInventory` call and no retry
loop were made in this frame.

A new, complete discovery page returned 24 unique managed nodes, all Online,
all Linux, all EC2Instance, in one page (0.461 seconds). All 24 were sampled
using one `AWS:Application` inventory page each (10.448 seconds total, about
0.44 seconds per node page). Every response contained zero entries and no
`CaptureTime`; no node truncated at 50 entries; zero errors and no throttling
occurred. The 24 responses totaled 1,872 bytes in compact JSON serialization;
this is not wire traffic or object heap size.

Frame C is a new snapshot, not a rerun of the 2026-10-28 frame: the count
differs (24 versus 22) and node identity equality cannot be verified because
neither frame retains identifiers. Two independent zero-capture frames now
agree, which strengthens the reading that no application inventory association
is collecting on these nodes, but it still does not distinguish an
unconfigured association from a completed empty capture, and it is not
evidence of zero installed packages. Under the rule above, all 24 responses
are unknown: they emit no application fact and cannot supersede any prior
snapshot. Stopped-node coverage, the inventory-enabled denominator, validated
package counts, capture cadence, change rate, and daily fact volume remain
NOT_PROVEN. Observed request bounds so far: discovery one page under one
second for ~24 nodes; one application page per node at about half a second
each with no throttling at this cohort size. Package-count and byte projections
stay symbolic until a captured cohort is measured.

### Association review attempt, 2026-10-29

One bounded read-only call tested the association-review path:

`aws ssm list-associations --association-filter-list key=Name,value=AWS-GatherSoftwareInventory --max-results 5`

It was denied: `AccessDeniedException`, identity
`arn:aws:sts::055929692761:assumed-role/eshu-dev-box/i-0e18f7b9871b85f57` is
not authorized to perform `ssm:ListAssociations`. No IAM change was made and
the call was not retried. Association status therefore cannot currently
distinguish unconfigured collection from empty capture; the association-review
path needs a `ssm:ListAssociations` (or equivalent describe) grant before it
can select a representative captured population. A denial is not a zero
inventory result.

### Grant re-verification, 2026-10-29 14:21 UTC

One bounded read-only call re-tested the path after the IAM update that had
enabled managed-node discovery:

`aws ssm list-associations --association-filter-list key=Name,value=AWS-GatherSoftwareInventory --max-results 5`

Still denied: `AccessDeniedException`, same identity
`arn:aws:sts::055929692761:assumed-role/eshu-dev-box/i-0e18f7b9871b85f57`,
`not authorized to perform: ssm:ListAssociations` on resource
`arn:aws:ssm:us-east-1:055929692761:*` because `no identity-based policy
allows the ssm:ListAssociations action`. Exit 254. No IAM change was made and
the call was not retried. The exact missing grant is `ssm:ListAssociations`
(or an equivalent association describe) on the QA measurement identity in
account 055929692761, region us-east-1; alternatively, an owner- and
arbiter-authorized QA-only inventory association with application collection
enabled, per the section below. Until one appears, the captured-cohort
measurement cannot run and the volume, limit, and cost numbers stay NOT_PROVEN.

### Captured-cohort measurement, 2026-10-29 22:07-22:34 UTC (FIRST POPULATED CAPTURE)

Full narrative (grant review, authorizations, association story, cleanup)
lives in [9302-ssm-captured-cohort-evidence.md](9302-ssm-captured-cohort-evidence.md):
no pre-existing capture existed, so the temporary QA-only association path
ran under owner + arbiter authorization (applications-only scope) and was
deleted afterwards. The first association failed on the agent rejecting
explicit `"Disabled"` for `files`; the retry omitted `files` and
`windowsRegistry` (document default, collects neither) with identical net
scope.

Capture (both targets, `AWS:Application`, dated `2026-10-29T22:31:19Z`,
measured ~2 minutes fresh):

- target A: 1310 entries over 27 pages, 601,671 response bytes (~459 B/entry, ~22.3 KB/page)
- target B: 1295 entries over 26 pages, 594,307 response bytes (~459 B/entry, ~22.9 KB/page)
- cohort totals: 2 nodes, 2605 entries, 53 pages, 1,195,978 bytes (~1.17 MB)

No raw rows entered any log; aggregates only. "Response bytes" throughout
this section means compact-JSON serialization of the `ListInventoryEntries`
responses, not wire traffic or object heap size (same byte method as the
earlier frames). Against the stated limits, arbiter-graded 2026-10-29: L1
(multi-page overflow) CONFIRMED by construction and observation — a populated
host is ~26-27 pages, so single-page reads are a correctness bug and
complete-pagination-before-supersession is mandatory; L2 retained as the
per-entry planning budget (~1 KB, ~2.2x headroom over the observed
~0.45 KB/entry); L3 retained as the per-page budget (45-50 KB, ~2x headroom
over the observed ~22-23 KB/page). S3 sync was never configured, so the cost
question stays bounded to API-read volume only: ~27 application pages and
~0.6 MB raw response per populated host per poll. Capacity planning must use
the conservative full-snapshot-per-poll bound until a second comparable
capture measures changed-vs-unchanged rate.

Non-application absence (closes the re-review's open check, detail in the
evidence file): 2026-10-29 ~23:00Z existence-only reads show zero entries
with no next page for `AWS:File`, `AWS:WindowsRegistry`, `AWS:Network`,
`AWS:Service`, and `AWS:Process` on both nodes, while `AWS:Application`
carries the exact measurement capture time. Canonical association form going
forward: omit `files` and `windowsRegistry` (document default, collects
neither), explicit `Disabled` for every other toggle type except
`applications`.

Binding numeric ceilings (arbiter-set): max 5,000 entries per host per
capture (~3.8x observed max); max 100 pages per host per type per poll;
1 KB/entry and 50 KB/page planning budgets; host set is an explicit
configured target list with no unlimited default. Implementation must fail
closed unless explicit positive values are configured and must not exceed
these ceilings without a new measurement. A capture exceeding any ceiling is
partial/unknown, never truncated-and-superseding. Freshness numbers
(stale-age, cross-type skew tied to the configured capture interval, and the
separate `DescribeInstanceInformation` platform rule) must be set in the
implementation issue before code lands.

Cleanup: association `01e99689-…` deleted 22:34:09Z (exit 0);
unpaginated ListAssociations afterwards returns empty (exit 0). Built-in
inventory data deletion is NOT claimed (retention unresolved, per the
earlier section).

</details>

## Implementation plan

After acceptance and #9301, implement typed contracts and registry in `eshu`,
collection in `eshu-hq/eshu-collector-aws-cloud`, and reducer/read paths in `eshu`. Keep
the source allowlist, partial-state diagnostics and proof plan above; do not
reuse image vulnerability findings or infer missing facts as deletion. Split
implementation into issues under #9295 only after the decision is accepted.

## Consequences

**Good**

- Host observations retain their source meaning and do not manufacture image
  or vulnerability evidence.
- Explicit partial and stale states preserve prior evidence on incomplete reads.

**Bad**

- New contracts and retention work are required for observations; #9209 owns
  the separate host impact correlation.
- Representative volume, supported version comparators and retained-data cleanup
  remain unknown; they may change the final design or operational cost.

**Cost and risk bounds (observed versus unmeasured)**

- Observed read-only probe cost, QA cohort of ~24 nodes: discovery one page
  under one second; about 0.44 seconds per node per `AWS:Application` page
  with no throttling; 1,872 bytes of compact-JSON responses for 24 zero-entry
  pages (serialization size, not wire or heap). One `GetInventory` attempt
  throttled after 5.2 seconds, so bounded per-node reads are the safer probe
  shape. No writes were performed by any measurement.
- Measured per-poll API-read volume (cohort of 2, 2026-10-29): ~27
  application pages and ~0.6 MB compact-JSON response per populated host per
  poll. Still unmeasured: fleet-scale request totals, ingestion and reducer
  work, storage and retention cost, host-agent overhead, changed-vs-unchanged
  rate, and dollar cost. The request bound is now concrete per host (hosts ×
  allowed pages per type per poll, within the ceilings above); fleet
  projections need the second capture.
- Risk: treating zero-capture frames as empty hosts would manufacture
  false-negative evidence; the unknown-not-empty rule above is load-bearing.
  A temporary QA association is an environment change needing owner and arbiter
  authorization with post-measurement deletion, and built-in inventory retention
  after deletion is unresolved. Do not promise a zero-cost outcome.

## Acceptance criteria

- [x] Measure a representative captured population with the denominators,
  counts, pages, bytes, timing, sampling limits and sizing bounds above
  (2-node cohort, 2026-10-29; fleet scale explicitly out of claim).
- [x] Settle kind names, stable keys, identity, IAM, privacy,
  partial-generation and host applicability rules (arbiter decision 1;
  exact JSON field spelling finalizes with the typed contracts in `eshu`;
  freshness numbers finalize in the implementation issue).
- [ ] Specify executable positive, negative and ambiguous fixture, concurrency,
  performance, graph, API and MCP checks with expected outcomes
  (moves to the implementation issues, with the required proof list in
  "Arbiter acceptance" below, including a ≥27-page no-truncation
  regression and a ceiling-breach partial-not-superseding test).
- [x] Obtain a final arbiter verdict on this completed decision before building
  (ACCEPT 2026-10-29, bounded observation-only first phase).

## Arbiter acceptance (2026-10-29, RULING: ACCEPT)

Bounded observation-only first phase accepted on the measured numbers; full
verdict with all 8 decisions in the evidence file. Binding takeaways:
ceilings above, API-read-volume-only cost with full-snapshot bound until the
second capture, freshness numbers in the implementation issue, canonical
omit-default association form, implementation splits under #9295 (never
inside closed PR #9452). Refused: fleet claims from n=2, rows-as-packages,
built-in data deletion, serialization/single-page reads. Re-escalate only on
ceiling breach, >1 KB/entry on a new host class, or non-application types
found collected (absent as of 2026-10-29 ~23:00Z).

## Decisions for the arbiter

1. Which exact versioned kind names, keys and schemas represent application
   observations and platform evidence?
2. Which captured cohort and numeric limits support this first observation-only
   implementation without making a fleet-wide capacity claim?
3. What measured age, clock-skew, and bounded resource limits govern a complete
   application snapshot and the separate platform observation?
4. Which discovery source and approved population provide representative
   capture for the limited cohort, and what is the retention policy?

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

Before accepting this record, resolve the host identity and stale semantics,
IAM and privacy scope, and measured volume. Host impact matching belongs to #9209.
An arbiter must accept the completed record before collector, contract, reducer,
or query implementation. File and work the accepted implementation issues in
this goal under #9295. This draft is not that acceptance.
