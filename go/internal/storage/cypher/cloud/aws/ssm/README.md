# SSM host inventory graph writers

Persists reducer-projected `SSMHostApplication` / `SSMHostPlatform`
observation nodes and `REPORTED_WITH` pairing edges (issue #9558).

## Statements

- Application and platform nodes MERGE by stable uid, then SET observation
  properties plus scope, generation, evidence source, and unix evidence
  times. MERGE-anchoring follows the #5652 fix. Uids are host-scoped with no
  scope component, so two scopes projecting one host flap the scope-owned
  properties last-write-wins; single-scope-per-host is supported.
- Pairing edges MATCH both endpoint observation nodes, then MERGE the
  `REPORTED_WITH` edge. Rows carrying any other relationship type are
  rejected before dispatch (closed vocabulary).
- Node writes dispatch before edge writes (separate transactions), so an edge
  MATCH never runs against nodes another statement in the same transaction
  has yet to make visible.
- The committed-capture read returns the newest committed application
  capture time per host as a unix int; no RFC3339 offset form can corrupt
  the comparison. It is one label-anchored `MATCH (n:SSMHostApplication)`,
  never a bare `MATCH (n)`; platform evidence needs no committed comparison
  and is not read back.
- Retracts are host- and evidence-scoped `DETACH DELETE`s: application nodes
  strictly older than the superseding capture, and listed conflicted platform
  uids. Retracts run sequential `Execute`, never `ExecuteGroup` (#4367).

## Telemetry

Writes flow through the shared executor/retry instrumentation and the
reducer domain's completion log. Retract statements carry the
`canonical_retract` operation and `ssm_host_inventory` phase metadata.
