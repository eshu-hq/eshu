# #9302 captured-cohort evidence log

Companion to [9302-ssm-inventory-source.md](9302-ssm-inventory-source.md),
which holds the normative design (limits, ceilings, acceptance). This file
holds the historical measurement narrative: commands, association story, and
the full arbiter verdict. Aggregates only; no raw inventory rows, no host
identifiers beyond what the authorizations required.

## Grant live and association review, 2026-10-29 evening UTC

After the owner added the grant to the role, the same bounded call now
succeeds: `aws ssm list-associations
--association-filter-list key=Name,value=AWS-GatherSoftwareInventory
--max-results 5 --region us-east-1` exits 0 with `{"Associations": []}`.
A bounded unfiltered review (`--max-results 10`, names and IDs only) also
exits 0 with an empty list: no associations of any document exist in
us-east-1 for this identity's view. No IAM change was made from here and
nothing was retried. Association targets and inventory content did not enter
logs beyond the empty counts above.

There was therefore no existing capture to select: the representative
captured-cohort measurement needed the temporary QA-only association path,
a separate environment change requiring owner and arbiter authorization
(targets, application-collection-only scope, and post-measurement deletion).

## Association story, 2026-10-29 22:07-22:34 UTC

Authorizations: owner approved the 4 named instances in-session; arbiter
REJECTED that set (none SSM-managed at the time) and pre-accepted a retry on
proven-managed targets. Owner then approved proceeding on the reporting
subset; 2 of the 4 came Online. Owner added a scoped create/delete grant to
the measurement role (CreateAssociation on the 2 instance ARNs + the
document ARN + the association ARN; Delete/DescribeAssociation on the
association ARN) after the first create attempt showed the grant missing the
association resource.

Association `463893ad-250e-4104-85ee-3c8bc6a80650` (created 22:07:23Z,
applications Enabled, all other types explicitly Disabled) executed 22:22:29Z
and Failed both targets: agent log shows the `files` plugin rejecting the
explicit `"Disabled"` value (`invalid character 'D' looking for beginning of
value`). The association was deleted (exit 0). It was replaced by
`01e99689-f60b-4bf8-8b38-23d86b63c92f` (created 22:29:59Z, identical scope
except `files` and `windowsRegistry` left at document default, which is
empty): "Disabled" is not a valid value for those two parameters, so the
letter of "explicitly disable everything" cannot be implemented; the net
collection scope is unchanged (applications only). Owner ran
start-associations-once under their own credentials (the measurement role
lacks that action).

Capture: both targets, `AWS:Application`, dated `2026-10-29T22:31:19Z`,
measured ~2 minutes fresh. Target A: 1310 entries over 27 pages, 601,671
response bytes (~459 B/entry, ~22.3 KB/page). Target B: 1295 entries over 26
pages, 594,307 response bytes (~459 B/entry, ~22.9 KB/page). Cohort totals:
2 nodes, 2605 entries, 53 pages, 1,195,978 bytes (~1.17 MB). "Response
bytes" means compact-JSON serialization of the `ListInventoryEntries`
responses, not wire traffic or object heap size.

Cleanup: association `01e99689-…` deleted 22:34:09Z (exit 0);
unpaginated ListAssociations afterwards returns empty (exit 0). Built-in
inventory data deletion is NOT claimed (retention unresolved).

## Non-application absence check, 2026-10-29 ~23:00Z

Existence-only `ListInventoryEntries` (`MaxResults=1`; only entry-presence,
NextToken-presence, and top-level CaptureTime recorded): on both measured
nodes, `AWS:File`, `AWS:WindowsRegistry`, `AWS:Network`, `AWS:Service`, and
`AWS:Process` all return zero entries with no next page and null capture
time, while `AWS:Application` returns entries with `CaptureTime
2026-10-29T22:31:19Z` — the exact measurement capture, which also proves
these two nodes are the measured cohort. Canonical association form going
forward: omit `files` and `windowsRegistry` (document default, which
collects neither — those parameters take JSON path specs, not
Enabled/Disabled toggles, which is why the agent rejected explicit
`"Disabled"`), explicit `Disabled` for every other toggle type except
`applications`.

## Arbiter acceptance, 2026-10-29 (RULING: ACCEPT)

Verified against branch head `f4ccbd7a51`: capture arithmetic (2605 entries,
53 pages, 1,195,978 B; ~459 B/entry; ~22.3-22.9 KB/page; pages equal
ceil(entries/50)); issue authorization trail 21:37Z-22:35Z; no
`aws_ssm`/`host_application`/`host_platform` kind yet in
`specs/fact-kind-registry.v1.yaml`; #9301 closed; PR #9452 closed unmerged.
Decisions: (1) design accepted bounded (observation-only first phase, new
v1.0.0 kinds `aws_ssm_host_application` / `aws_ssm_host_platform`, no
vulnerability or supply-chain findings, #9209 keeps correlation, online EC2
managed nodes with confirmed capture only); (2) L1/L2/L3 settled at cohort
scale as forward budgets; (3) binding ceilings (5,000 entries/host/capture,
100 pages/host/type/poll, 1 KB/entry, 50 KB/page, explicit target list, fail
closed, exceed = partial/unknown never truncated-and-superseding); (4) cost
settled for API-read volume only, full-snapshot bound until the second
capture, unchanged-`CaptureTime` polls suppressed or proven idempotent
before any dedup claim; (5) freshness as conditions, numbers in the
implementation issue; (6) files/windowsRegistry omission-as-default is the
canonical form; (7) the post-verdict reconciliation (editorial, measurement
intact); (8) implementation splits under #9295, never inside #9452.
Refused: fleet capacity/cost from n=2; application rows as OS packages;
built-in data deletion; serialization or single-page reads. Re-escalate only
if a populated host exceeds the ceilings, observed bytes/entry exceed 1 KB
on a new host class, or a non-application type is found collected on the
measured nodes.
