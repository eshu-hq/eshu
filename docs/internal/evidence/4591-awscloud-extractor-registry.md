# Evidence: awscloud typed-depth extractor registry (#4591)

Issue [#4591](https://github.com/eshu-hq/eshu/issues/4591) asks `awscloud` to
converge on the per-resource-type extractor registry `gcpcloud` already uses.
Its acceptance was one scanner per PR with byte-identical fact output proven by
fixture parity. That sequencing only works if the registry exists first, so this
change lands the registry alone and migrates no scanner.

## Correction: the scanner migrations this planned are not the work

Recorded after the fact, so the plan above is not followed by mistake.

The per-scanner migration sequence this evidence describes is wrong, and would
have produced a run of changes that alter nothing while passing their own
parity check. `ExtractContext.Data` is a `json.RawMessage`, a raw per-resource
provider payload. AWS scanners never hold one: their clients return typed Go
structs, and each scanner already fills `Attributes` and `CorrelationAnchors`
per resource type straight into a `ResourceObservation` that reaches a fact
envelope through `NewResourceEnvelope`. Migrating a scanner would mean
marshalling a typed struct back to JSON so an extractor could parse it again,
and because nothing dispatches through `resourceExtractors`, fact output could
not change either way. Fixture parity would pass because nothing happened.

The registry is kept, because AWS Config ingestion is planned and is the
producer it fits: a Config `configurationItem` carries a `configuration` blob of
raw per-resource-type JSON, the same shape Cloud Asset Inventory hands
`gcpcloud`, and one parse loop over that feed needs per-resource-type dispatch.
Extractors get registered when that lane lands.

Worth stating plainly, since #4591's premise rested on it: the cross-provider
contract already matches. `facts.Envelope`, stable IDs, the fact-kind registry
with a JSON Schema per kind, redaction rules, and the
`attributes` + `correlation_anchors` + typed-relationships convention are shared
by `aws_resource` and `gcp_cloud_resource` alike, and the payload-typing work
#4591 named as its prerequisite (#4568) is closed. Nothing downstream can tell
whether attributes were filled by a registered extractor or a scanner function.
What differs upstream is the input: one generic feed for `gcpcloud`, typed SDK
calls per service for `awscloud`.

No-Regression Evidence: nothing is registered, so no scanner reaches the new
code. `extractResourceAttributes` returns `handled=false` with a nil error and a
zero-value `AttributeExtraction` for every resource type, because
`resourceExtractors` is empty and only `init` functions in per-type files may
populate it. Every existing awscloud scanner therefore emits exactly the facts
it emitted before, on the same path, with the same call sequence. There is no
new allocation, lookup, or branch on any live emission path: the map is not
consulted unless a caller invokes the new dispatch helper, and no caller does
yet. `cd go && go test ./internal/collector/awscloud/ -count=1` passes for the
whole package (not only the new tests), and `cd go && go vet
./internal/collector/awscloud/` is clean. The two added files are additive; no
existing file is modified.

Benchmark Evidence: none is meaningful for this change and none is claimed. A
registry with zero registrations has no measurable hot path — the only new work
executable today is a map lookup that no production caller performs. Measuring
an empty registry would manufacture a number rather than establish one. Cost
becomes measurable when the AWS Config lane dispatches through it on a real
feed, and belongs to that change.

Observability Evidence: this change registers no metric, span, or log line. Its
row in `docs/public/observability/telemetry-coverage.md` carries the
`No-Observability-Change:` marker and names the signals that already cover AWS
emission — `eshu_dp_aws_resources_emitted_total` and
`eshu_dp_aws_scan_duration_seconds` from the AWS cloud row
(`go/internal/collector/awscloud/awsruntime/source.go:177`), plus
`eshu_dp_facts_emitted_total` and `eshu_dp_facts_committed_total` from the fact
commit row (`go/internal/collector/git_source_processing.go:217`). Those stay
correct when the AWS Config lane registers extractors, because extraction fills
attributes on the existing observation path rather than changing what is emitted
or how it is committed.
`bash scripts/verify-telemetry-coverage.sh` exits 0.

## Design decision recorded here so it is not re-opened

The AWS types are declared in `awscloud` rather than shared with `gcpcloud`.
`gcpcloud.ExtractContext` is CAI-shaped — full resource name, asset type,
project id — and its relationship endpoints are CAI full resource names. AWS
identity is ARN-shaped and scoped by account and region. Importing one collector
package into the other to share three field names would couple two provider
surfaces whose identity models differ, and would force any future divergence in
one provider's context to be negotiated against the other. Two small
declarations that can drift independently are the cheaper trade. If a third
provider arrives and all three genuinely agree, that is the point to extract a
shared type — not before.

## What the tests pin

Written failing-first against undefined symbols, then implemented:

- round-trip: a registered extractor's `Attributes` and `CorrelationAnchors`
  survive dispatch
- an unregistered type is `handled=false` with a nil error and a zero value —
  the property that lets the Config lane add extractors one resource type at a
  time without stranding the types it has not covered yet
- an extractor error is wrapped so it names its resource type, asserted against
  a payload carrying a fake password to prove the resource data does not leak
  into the error string
- whitespace tolerance on both registration and lookup, so a stray space in a
  constant cannot silently disable an extractor
- all three panic paths (blank type, nil extractor, duplicate registration), so
  a wiring mistake fails loudly at init instead of shadowing another type
