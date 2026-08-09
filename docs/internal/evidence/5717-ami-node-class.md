# Evidence: #5717 — EC2 AMI node class resolves the instance->AMI relationship

Issue #5448 shipped an `ec2_instance_uses_ami` `aws_relationship` fact
(EC2 instance -> AMI), but emitted no `aws_resource` fact for the AMI itself.
The generic AWS relationship edge join
(`buildCloudResourceJoinIndex`/`resolveTarget`,
`go/internal/reducer/aws_relationship_join.go`) resolves a relationship
target only against an already-indexed `aws_resource` fact, so this edge's
target NEVER resolved: it always fell through to `joinModeUnresolved`, was
counted in the tally, and the row was dropped before
`WriteCloudResourceEdges` ever ran. #5717 closes that gap by materializing
the AMI as a node.

## Design implemented

**Pattern A: the existing `:CloudResource` label, not a new node class.** The
AMI materializes with `resource_type=aws_ec2_ami` under the SAME label every
other AWS resource type uses. No new Cypher label, no dedicated node/edge
writer, no `graphEntityKinds` facet entry, and no #5472 registration
(retractable_edge_types, replay-depth spec, graph schema uid constraint): the
generic `aws_resource_materialization` domain
(`go/internal/reducer/aws_resource_materialization.go`,
`ExtractCloudResourceNodeRows`/`cloudResourceNodeRow`) and the generic
relationship-edge join both already handle any `resource_type` that is not
explicitly excluded (only `aws_ec2_instance` is excluded, for the unrelated
dual-writer reason documented at `cloudResourceNodeRow`'s doc comment).

A dedicated `:MachineImage` label (mirroring the #5450 `:ContainerImage`
pattern) was considered and rejected: it would need its own node/edge writer,
a `graphEntityKinds` facet entry, and #5472 registration, and the generic
`CloudResourceEdgeWriter`'s hardcoded `MATCH (target:CloudResource ...)`
could not match it without a second, parallel edge-writer path. Pattern A
needed none of that — once the `aws_resource` fact for the AMI exists, the
edge resolves through code that already runs today.

**No `DescribeImages` call.** The AMI resource fact carries ONLY identity
(account_id, region, resource_id=AMI id, with Name set to the bare resource id
like every other EC2 resource type) — no rich state, owner, or creation-date. That AMI metadata lives on the separate `DescribeImages` API,
which this increment deliberately does not call: the issue's scope is "make
the edge resolve," not "enrich the AMI node." Enrichment is a distinct,
separately costed follow-up. This is a documented scope boundary, not an
oversight — see the doc comments on `amiResourceObservation`
(`go/internal/collector/awscloud/services/ec2/ami_identity.go`).

**Deduplicated per scan.** Many EC2 instances commonly launch from the same
AMI. `amiResourceEnvelopes` emits the AMI's `aws_resource` fact only the
first time a given AMI id is seen within one `Scanner.Scan` call
(`seenAMIIDs` map in `scanner.go`), not once per instance. Proven by
`TestScannerDedupesAMIResourceFactAcrossSharedInstances`.

**Contract System v1 alignment.** `sdk/go/factschema/aws/v1.ResourceTypeEC2AMI`
is the new canonical constant; `awscloud.ResourceTypeEC2AMI` now aliases it
(`constants_ec2.go`), matching every sibling AWS resource-type constant. It
was previously a raw local string literal, the one outlier in the family.

## Pushback on the original design brief

None on the substance of the chosen design (Pattern A, no `DescribeImages`
call, per-scan dedup) — all three were implemented as specified and are
believed correct.

One correction to the brief's TDD expectation: the brief asked for a reducer
test proving `ExtractAWSRelationshipEdgeRows` "now RESOLVES... it must go RED
on current main." That test
(`TestExtractAWSRelationshipEdgeRowsResolvesEC2InstanceUsesAMITarget`,
`go/internal/reducer/aws_relationship_join_test.go`) is included as a
regression guard, but it does **not** go red on unmodified `main`: it seeds
the AMI's `aws_resource` fact directly as a hand-built fixture (the same
pattern every other test in that file uses), and `buildCloudResourceJoinIndex`
already indexes any `aws_resource` fact generically, with no special-casing
by `resource_type` (unlike `cloudResourceNodeRow`'s deliberate
`aws_ec2_instance` exclusion). The reducer join layer needed **zero code
changes** — the entire fix is collector-side (the AMI fact was simply never
emitted before). The true "issue is fixed, provably" regression is the
golden-corpus layer: rc-174 below is provably 0 on unmodified `main` (no AMI
`aws_resource` fact exists in the cassette before this change, so the join
never resolves the real corpus's edge), and only passes once both the
collector fact-emission fix AND the cassette update land together. The
reducer-layer test is retained anyway as a documented regression guard
against a future accidental re-introduction of a `resource_type`
special-case in the join index (mirroring `cloudResourceNodeRow`'s existing
`aws_ec2_instance` exclusion) — see the test's own doc comment for this
distinction.

## Layer-by-layer TDD evidence

### Collector layer (RED before, GREEN after)

RED, on the code before `ami_identity.go`/`scanner.go` were changed (three
new/modified assertions, run against the collector with the test file
changes already applied but the production wiring not yet added):

```
=== RUN   TestScannerEmitsInstancePostureAndIdentityFacts
    scanner_test.go:194: aws_resource count = 1, want 2 (#5448 identity fact + #5717 AMI resource fact)
--- FAIL: TestScannerEmitsInstancePostureAndIdentityFacts (0.00s)
=== RUN   TestScannerDedupesAMIResourceFactAcrossSharedInstances
    scanner_test.go:321: aws_resource count = 2, want 3 (2 instance identities + 1 deduped AMI resource fact)
--- FAIL: TestScannerDedupesAMIResourceFactAcrossSharedInstances (0.00s)
FAIL
```

(`TestScannerEmitsIdentityWithoutAMIRelationshipWhenImageIDBlank` already
passed at this point since the blank-ImageID path had nothing to add.)

GREEN after `ami_identity.go` (new file: `amiResourceObservation`,
`amiResourceEnvelopes`) and the `scanner.go` wiring (`seenAMIIDs` map,
per-instance `amiResourceEnvelopes` call) landed:

```
ok  	github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ec2	...
```

### Reducer/join layer

`TestExtractAWSRelationshipEdgeRowsResolvesEC2InstanceUsesAMITarget` passes
unconditionally (see Pushback above for why it is not a RED-then-GREEN proof
at this layer): given the AMI's `aws_resource` fact, the target resolves by
bare id (`joinModeBareID`), with `source_uid`/`target_uid` matching
`cloudResourceUID` computed independently in the test.

### Node materialization layer

No new test was needed: `cloudResourceNodeRow`'s existing
`resource.ResourceType == awsv1.ResourceTypeEC2Instance` exclusion (the only
special-case in that function) does not match `aws_ec2_ami`, so the AMI row
flows through the ordinary path unmodified. This is exercised end-to-end by
the golden-corpus gate's `rn-ec2-ami-node` assertion (below), not a new unit
test, because the node-row-building logic itself required no code change —
only new input (the AMI fact) that a generic path already handles.
`TestReducerAWSResourceTypeConstantsMatchFactSchema` and the sibling
`awscloud` contract test
(`TestAWSCloudResourceTypeConstantsMatchFactSchema`, new `ec2_ami` case) both
prove the `aws_ec2_ami` string constant stays in lockstep between the
collector, the reducer, and the SDK schema.

## Golden-corpus updates

- `testdata/cassettes/awscloud/supply-chain-demo.json`: added one
  `aws_resource` fact to the `aws:123456789012:us-east-1:ec2` scope
  (`resource_type: aws_ec2_ami`, `resource_id: ami-000000000000000a` — the
  cassette's existing synthetic AMI id, already referenced by the #5448
  relationship fact and the instance's `ami_id` attribute). `arn`/`state` are
  empty strings (not omitted), matching exactly what
  `awscloud.NewResourceEnvelope` emits for `amiResourceObservation`'s
  observation (no ARN, no state set).
- `testdata/golden/e2e-20repo-snapshot.json`:
  - `node_counts.CloudResource`: floor 117->118, ceiling 123->124 (exactly
    one new node — the corpus is deterministic and this cassette carries
    exactly one instance and one distinct AMI id). Note updated to name the
    new `ec2.ami` fixture and cross-reference rc-174/rn-ec2-ami-node.
  - `required_nodes`: added `rn-ec2-ami-node` (CloudResource,
    `resource_type=aws_ec2_ami`, min/max 1 — the exact ceiling catches a
    duplicate-row regression of the dedup contract, not just a floor miss).
  - `required_correlations`: added `rc-174`
    ((CloudResource)-[:AWS_ec2_instance_uses_ami]->(CloudResource),
    minimum_count 1) — **the non-vacuous proof the issue is fixed**. This
    assertion is provably 0 on unmodified `main` (no AMI `aws_resource` fact
    in the cassette, so the join never resolves) and only passes with both
    the collector fix and the cassette update present together. No
    `evidence_kinds` predicate is needed: `AWS_ec2_instance_uses_ami` (the
    `CloudResourceEdgeWriter`'s `AWS_<relationship_type>` convention) is
    already a fully verb-specific Cypher relationship type, not a shared
    generic one.
- `go/cmd/golden-corpus-gate/io_seams_test.go`: `fileLanguageFloor()`'s
  `CloudResource|resource_type` fixture list gained `"aws_ec2_ami"`, matching
  the new `rn-ec2-ami-node` assertion — `EvaluateRequiredNode` is
  unconditionally `Required: true` (unlike `required_correlations`, which has
  an advisory/blocking split), so this fixture update was required for
  `TestCheckGraphRequiredOnlyPassesOnExistence` to stay green; the test
  failure this fixture gap caused, and the fix, are cited under Verification
  below.

Every changed count was derived from the cassette diff (exactly one new
`aws_resource` fact => exactly one new `CloudResource` node in a deterministic
corpus), never assumed.

## Verification (this session, in the feature worktree)

```
cd go
go build ./...                                                     # exit 0
go vet ./...                                                       # exit 0
go test ./internal/collector/awscloud/... ./internal/reducer/... \
  ./internal/query/... ./internal/mcp/... -count=1                 # exit 0 (all ok, no FAIL)
go test ./cmd/golden-corpus-gate/... -count=1                      # exit 0 (after the
                                                                     # io_seams_test.go fixture
                                                                     # update above; failed
                                                                     # with 2 FAILs before it)
```

`bash scripts/test-verify-golden-corpus-gate.sh` (static/no-Docker contract
check) also passed: `test-verify-golden-corpus-gate: pass`.

The live B-7 golden-corpus gate (`scripts/verify-golden-corpus-gate.sh`,
Docker-backed) was intentionally NOT run in this session — that is the
orchestrator's step, not a sub-agent's, per this repo's live-gate
serialization rule.

No-Regression Evidence: the change is additive-only at every layer (a new
fact kind of an existing type, a new node under an existing label, one new
required_correlation and one new required_node in the snapshot); no existing
Cypher, writer, or query code path changed. `go test
./internal/collector/awscloud/... ./internal/reducer/... ./internal/query/...
./internal/mcp/... -count=1` above covers every existing package these files
live in with no failures.

No-Observability-Change: no metric, span, log field, or `aws_scan_status`
column changes. The collector emits facts only (no new instrument), and the
reducer's existing `aws_resource_materialization`/`aws_relationship_join`
completion logs and `eshu_dp_aws_relationship_edges_total` counter already
cover the AMI's node write and edge resolution through their existing
`resource_type`/`join_mode` dimensions — no new dimension value was added to
either metric's bounded label set.
