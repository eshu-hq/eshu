# Evidence: awscloud typed-depth extractor registry (#4591)

Issue [#4591](https://github.com/eshu-hq/eshu/issues/4591) asks `awscloud` to
converge on the per-resource-type extractor registry `gcpcloud` already uses.
Its acceptance is one scanner per PR with byte-identical fact output proven by
fixture parity. That sequencing only works if the registry exists first, so this
change lands the registry alone and migrates no scanner.

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
executable today is a map lookup that no production caller performs. The
per-scanner migrations that follow are where fact-output parity and cost matter,
and #4591 already binds each of those to fixture parity in its own PR. Measuring
an empty registry would manufacture a number rather than establish one.

Observability Evidence: this change registers no metric, span, or log line. Its
row in `docs/public/observability/telemetry-coverage.md` carries the
`No-Observability-Change:` marker and names the signals that already cover AWS
emission — `eshu_dp_aws_resources_emitted_total` and
`eshu_dp_aws_scan_duration_seconds` from the AWS cloud row
(`go/internal/collector/awscloud/awsruntime/source.go:177`), plus
`eshu_dp_facts_emitted_total` and `eshu_dp_facts_committed_total` from the fact
commit row (`go/internal/collector/git_source_processing.go:217`). Those stay
correct through the migrations, because moving where attributes are built does
not change what is emitted or how it is committed.
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
  the property that makes the migration incremental
- an extractor error is wrapped so it names its resource type, asserted against
  a payload carrying a fake password to prove the resource data does not leak
  into the error string
- whitespace tolerance on both registration and lookup, so a stray space in a
  constant cannot silently disable an extractor
- all three panic paths (blank type, nil extractor, duplicate registration), so
  a wiring mistake fails loudly at init instead of shadowing another type
