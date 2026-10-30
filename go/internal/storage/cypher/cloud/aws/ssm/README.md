# SSM host inventory graph writers

Persists reducer-projected `SSMHostApplication` / `SSMHostPlatform`
observation nodes and `REPORTED_WITH` pairing edges (issue #9558).

## Statements

- Application and platform nodes MERGE by stable uid, then SET observation
  properties plus scope, generation, evidence source, and unix evidence
  times. MERGE-anchoring follows the #5652 fix.
- Pairing edges MATCH both endpoint observation nodes, then MERGE the
  `REPORTED_WITH` edge. Rows carrying any other relationship type are
  rejected before dispatch (closed vocabulary).
- Node writes dispatch before edge writes (separate transactions), so an edge
  MATCH never runs against nodes another statement in the same transaction
  has yet to make visible.
- The committed-capture read returns newest committed evidence times per host
  as unix ints; no RFC3339 offset form can corrupt the comparison.
- Retracts are host- and evidence-scoped `DETACH DELETE`s: application nodes
  strictly older than the superseding capture, and listed conflicted platform
  uids. Retracts run sequential `Execute`, never `ExecuteGroup` (#4367).

## Performance Evidence

New write path (no baseline to regress): batches ride `UNWIND` with the
default batch size; node MERGEs seek the `SSMHostApplication_uid_unique` /
`SSMHostPlatform_uid_unique` constraints added with this change; the
committed-capture read runs once per intent (not per host); retracts are
host-scoped. No existing statement, phase order, or hot path is touched.

## Observability Evidence

No new instrument: writes flow through the existing executor/retry
instrumentation and the reducer domain's completion log. Retract statements
carry the `canonical_retract` operation and the `ssm_host_inventory` phase
metadata.
