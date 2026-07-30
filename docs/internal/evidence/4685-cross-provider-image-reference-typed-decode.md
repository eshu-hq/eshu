# Cross-Provider image_reference Typed Decode Evidence (#4685)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Cross-Provider image_reference Typed Decode (#4685)

The `container_image_identity` domain read every provider's image_reference
evidence through raw `payloadStr(...)`/map lookups: `addAWSImageReference`,
`addAzureImageReference`, `addGCPImageReference`, plus the ci.artifact,
ci.workflow_image_evidence, and ci.run reads in
`container_image_identity_evidence.go`. This migrates the whole shared family
to the `sdk/go/factschema` typed decode seam in one change (the per-cloud waves
#4568/#4632 deliberately deferred these kinds: typing one provider while the
shared consumer still read the others raw would have left the typed structs as
hollow contracts). Each read now decodes through a `Decode…` wrapper
(`factschema_decode_imagereference.go`, `decodeCICDRun`/`decodeCICDArtifact`/
`decodeCICDWorkflowImageEvidence`) routed through `partitionDecodeFailures`, so
a fact missing a required identity field is quarantined as a per-fact
`input_invalid` dead-letter instead of building a malformed image reference from
empty-string segments (e.g. `123456789012.dkr.ecr..amazonaws.com/team/api@…`
when `region` is absent). The typed evidence extractors moved to
`container_image_identity_typed_evidence.go` to keep both files under the
500-line cap. The now-unused raw `cicdRunKey` map reader was deleted.

No-Regression Evidence: this is an output-preserving migration for valid facts.
The B-7 golden-corpus gate stays green with zero drift —
`COMPOSE_PROJECT_NAME=gc4685 ESHU_POSTGRES_PORT=55433 NEO4J_BOLT_PORT=57688
NEO4J_HTTP_PORT=57475 GATE_API_PORT=58081 GATE_MCP_PORT=58091 bash
scripts/verify-golden-corpus-gate.sh` reported `417 pass, 0 required-fail,
0 advisory-warn` and `PASS: B-7 golden corpus gate green`, proving the
projected graph and query truth for valid image_reference facts is byte-
identical (no B-12 snapshot change). Behavior proof (failing-then-green):
`go test ./internal/reducer -run
TestContainerImageIdentityHandlerQuarantines -count=1` — the six per-provider
quarantine tests (aws missing region, azure missing owning_arm_resource_id,
gcp missing owning_full_resource_name, ci.artifact missing run_id,
ci.workflow_image_evidence missing repository_id, ci.run missing run_id) each
fail red against the pre-migration raw-lookup code (`input_invalid_facts = 0`,
a silent empty identity) and pass green after the seam routing;
`TestContainerImageIdentityHandlerQuarantineReplayIsIdempotent` proves the
quarantine decision is deterministic across replays. `go test
./internal/reducer ./internal/payloadusage ./cmd/reducer -count=1` and
`cd sdk/go/factschema && go test . -count=1` stay green. The #4573
payload-usage manifest grows from 108 to 111 governed struct-backed fact kinds
(the three cloud image_reference kinds now decode through a typed seam and are
mapped in `go/internal/payloadusage/schema.go`; the OCI and CI/CD image kinds
this domain also reads were already mapped under their own families).

No-Observability-Change: a quarantined malformed image_reference fact feeds the
existing `eshu_dp_reducer_input_invalid_facts_total` counter (labeled
`domain`=`container_image_identity`, an existing label value), the structured
`reducer input_invalid fact quarantined` log, and
`Result.SubSignals["input_invalid_facts"]`. Normal runs of the domain remain
covered by `eshu_dp_container_image_identity_decisions_total` plus
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.
The decode wrappers and typed evidence extractors add no new operational stage
and emit no metric of their own.
