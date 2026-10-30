# SSM host inventory materialization

Projects admitted `aws_ssm_host_application` / `aws_ssm_host_platform` facts
(issues #9302, #9558) into canonical observation graph nodes and
`REPORTED_WITH` pairing edges.

## Flow

`MaterializationHandler.Handle` loads one scope generation's SSM facts,
decodes them through the strict payload seam (`payload.go`), assesses each
host's streams (`rows.go` over `facts/cloud` verdicts), reads the committed
captures back from the graph, and writes plus retracts only for superseding
hosts. Preserved hosts are untouched; the verdict stays visible on the
result's evidence summary.

## Rules that must stay true

- Only a fresh complete application capture newer than the committed one
  supersedes. Mixed captures, aged captures, delayed duplicates, and silent
  generations preserve prior truth.
- Same-capture rows merge; only strictly older captures retract. A truncated
  re-read of one capture can never delete its siblings.
- Conflicting platform content for one host and provenance retracts the prior
  node and projects nothing; the coherent application capture still
  supersedes. No winner is picked by ordering.
- Status is derived at read time, never stored on graph nodes.
- No vulnerability, supply-chain, PURL, or image-digest derivation. Ever.
- The payload field spelling and stable-key encoding stay byte-identical to
  the #9556 contracts until the eshu-sdk release carrying these kinds moves
  this seam onto `schemadecode`. The oracle tests pin the encoding.

## Telemetry

No new instruments: malformed facts flow through the existing
`ReducerInputInvalidFacts` counter via `RecordQuarantinedFacts`, and the
domain gains values on the existing per-domain execution counters. Every run
logs the `ssm host inventory materialization completed` completion line with
host, supersede/preserve, write, retract, and stage-duration fields.
