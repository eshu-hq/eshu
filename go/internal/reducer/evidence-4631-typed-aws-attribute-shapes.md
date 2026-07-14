# Evidence: typed AWS resource/relationship attribute shapes (#4631)

Scope: `go/internal/reducer/ec2_block_device_kms_posture_index.go`,
`observability_coverage_correlation_index.go`,
`workload_cloud_relationship_materialization.go`,
`aws_resource_service_anchor.go`, `aws_resource_materialization.go`,
`iam_instance_profile_role_edge_rows.go`, and `factschema_decode.go` change how
seven service-specific `aws_resource`/`aws_relationship` attribute shapes are
read: each raw `payloadString`/`payloadStrings`/`payloadAttributes` map lookup
is replaced with a typed decode call into a new
`sdk/go/factschema/aws/v1/attribute_shapes.go` accessor
(`DecodeResourceEC2VolumeAttributes`, `DecodeResourceKMSKeyAttributes`,
`DecodeResourceIAMInstanceProfileAttributes`,
`DecodeRelationshipCloudWatchAlarmObservesMetricAttributes`,
`DecodeRelationshipXRaySamplingRuleMatchesServiceAttributes`,
`DecodeResourceAnchorAttributes`, `DecodeResourceNestedAnchorAttributes`). The
CI hot-path gate flags any change under `go/internal/reducer/` by location, so
this file records the required evidence.

## What changed

Every one of the seven shapes was already read from the decoded
`awsv1.Resource`/`awsv1.Relationship` struct's untyped `Attributes`
pass-through (never the raw envelope payload) before this change; this PR does
not add a new read site, a new query, a new lock, a new worker, or a new graph
write. It replaces a map-lookup-plus-coercion (`payloadString` silently
`fmt.Sprint`-stringifies a present wrong-typed value; `payloadStrings`
similarly coerces non-string slice entries) with a strict JSON-type-checked
decode that returns a typed struct for a valid value and a classified
`*awsv1.AttributeShapeError` for a present-but-wrong-typed value. Each call
site quarantines that error as a per-fact `input_invalid` dead-letter (the
same visible dead-letter surface a missing required identity field already
uses), instead of silently propagating a coerced or dropped value into the
projected graph.

No new Cypher, no new graph writes, no worker/lease/batch/concurrency logic
change, no new query shape, no runtime/Compose/Helm setting change. Every
touched extractor keeps its existing bounded in-memory index and O(1)-per-fact
resolution; the decode itself is a handful of map lookups and type assertions
per field, the same order of work the raw lookups it replaces already did.

No-Regression Evidence: the change swaps N raw map-lookup-plus-coercion calls
for N typed-decode calls doing the same map lookups plus a type assertion —
same O(1)-per-field cost, no added allocation, traversal, lock, or query work,
and backend-neutral (nothing here reaches Postgres or the graph backend). This
is a correctness fix for the malformed-value case, so the intended behavior
delta is that a present-but-wrong-typed service-specific attribute now
dead-letters visibly instead of silently coercing (`payloadString`'s
`fmt.Sprint`) or silently dropping (a non-map `[]any` entry) into a wrong
projected value. Proven by failing-then-green regression tests against each of
the seven shapes: before the fix a malformed `encrypted`/`key_manager`/
`role_arns`/dimension `value`/`service_name`/`environment` value produced a
misclassified or silently-empty result with zero quarantine signal; after the
fix each of `TestExtractEC2BlockDeviceKMSPostureRowsMalformedEncryptedQuarantines`,
`TestExtractEC2BlockDeviceKMSPostureRowsMalformedKeyManagerQuarantines`,
`TestExtractIAMInstanceProfileRoleEdgeRowsMalformedRoleARNsQuarantines`,
`TestBuildObservabilityCoverageDecisionsMalformedDimensionValueQuarantines`,
`TestBuildObservabilityCoverageDecisionsMalformedXRayServiceNameQuarantines`,
`TestExtractWorkloadCloudRelationshipRowsMalformedEnvironmentQuarantines`, and
`TestExtractCloudResourceNodeRowsMalformedServiceNameQuarantines` assert exactly
one `input_invalid` quarantine and no silently-wrong row. The full
`./internal/reducer` package passes (including every pre-existing valid-fact
test unchanged), `./internal/reducer -race` passes, `sdk/go/factschema` and
`sdk/go/factschema/aws/v1` pass, the payload-usage manifest gate
(`TestPayloadUsageManifest`) is unaffected (the new accessors decode an
already-decoded struct's Attributes field, not a `facts.Envelope`, so they are
not a new decode seam the manifest counts), and the B-7 golden-corpus gate ran
green end to end (417 pass, 0 required-fail, 0 advisory-warn, elapsed 34s
against the 30m ceiling) with no snapshot or cassette delta — the golden
corpus carries only valid facts for every touched resource_type/verb, so this
is the byte-identical-for-valid-input proof the design's bounded-subset typing
requires.

No-Observability-Change: no metrics, spans, or logs are added or altered by
this change itself. Each malformed-attribute quarantine flows through the
pre-existing `recordQuarantinedFacts` dead-letter path (the same
`eshu_dp_reducer_input_invalid_facts_total` counter and structured error log a
missing required field already uses), so a malformed service-specific
attribute becomes visible on the same dashboard/log surface an operator already
watches for input_invalid facts, without a new instrument.
