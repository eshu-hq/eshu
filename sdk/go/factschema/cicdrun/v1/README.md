# CI/CD Run Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`ci_cd_run` fact family's seven fact kinds with a reducer decode seam, part
of the public `github.com/eshu-hq/eshu/sdk/go/factschema` Go module
(Contract System v1 §3.1).

## Kinds

| Fact kind | Go type | Required fields |
|---|---|---|
| `ci.run` | `Run` | `provider`, `run_id` |
| `ci.artifact` | `Artifact` | `provider`, `run_id` |
| `ci.environment_observation` | `EnvironmentObservation` | `provider`, `run_id` |
| `ci.deployment_event` | `DeploymentEvent` | `provider`, `deployment_id`, `environment`, `sha` |
| `ci.trigger_edge` | `TriggerEdge` | `provider`, `run_id` |
| `ci.step` | `Step` | `provider`, `run_id` |
| `ci.workflow_image_evidence` | `WorkflowImageEvidence` | `repository_id` |

`ci.deployment_event` is contract-layer-only today: it has a reducer decode
seam (`decodeCICDDeploymentEvent`,
`go/internal/reducer/factschema_decode_cicdrun.go`) so this gate-facing
package types it, but no `ci_cd_run_correlation` domain reads the decoded
struct yet — unlike the other six kinds below, its required fields are not
(yet) a reducer join key.

Every required field on the six correlation-consumed kinds above is a
reducer join-key segment: `provider` + `run_id` (+ `run_attempt`, which
defaults to `"1"` and stays optional) key the reducer's `cicdRunEvidence` map
(`go/internal/reducer/ci_cd_run_correlation.go:cicdRunKey`), and
`repository_id` is the sole key `attachWorkflowImagesToRuns`
(`go/internal/reducer/ci_cd_run_correlation_workflow_image.go`) uses to attach
workflow image evidence to a run. A fact missing its required field could
never join correctly under the pre-typing raw-map read, so the typed decode
seam now dead-letters it as a per-fact `input_invalid` quarantine instead of
silently producing an empty-string join key. `ci.deployment_event`'s required
fields (`provider`, `deployment_id`, `environment`, `sha`) are required
because GitHub's Deployments API always returns all four, not because a
reducer join key reads them yet — see `DeploymentEvent`'s own godoc.

## Deferred kinds

`ci.job`, `ci.pipeline_definition`, and `ci.warning` are emitted by the
collector (`go/internal/facts.CICDRunFactKinds()`) but have no reducer or
storage decode call today — `cicdRunCorrelationFactKinds()` does not even load
`ci.job`/`ci.pipeline_definition`. They are intentionally NOT typed here,
matching how other families (sbom_attestation, vulnerability_intelligence)
leave an emitted-but-unread kind typed only when its consumer lands.

## Two collector origins, one family

`Run`, `Artifact`, `EnvironmentObservation`, `TriggerEdge`, and `Step` are
emitted by the `ci_cd_run` collector's GitHub Actions provider path
(`go/internal/collector/cicdrun`). `WorkflowImageEvidence` is emitted by a
DIFFERENT collector — the git collector's static workflow-file scanner
(`go/internal/collector/git_workflow_image_facts.go`) — but shares the
`ci.workflow_image_evidence` fact kind and `ci_cd_run` schema version
(`facts.CICDSchemaVersion`), and the reducer's `ci_cd_run_correlation` domain
reads both origins together. It lives in this package because it is part of
the same reducer-consumed family, not because it shares a collector.

`DeploymentEvent` has no collector emitter yet — only the contract layer
(struct, schema, decode seam) exists today. Wiring a collector to emit
`ci.deployment_event` facts is later follow-on work, out of scope for this
change.

## Contract Rules

See `AGENTS.md` in this directory for the full rule set (required-field
derivation, schema regeneration, and the join-key rationale per kind).
