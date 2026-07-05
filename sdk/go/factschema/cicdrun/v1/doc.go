// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "ci_cd_run" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_cicdrun.go).
//
// Six fact kinds live here, all consumed by the reducer's
// ci_cd_run_correlation domain (go/internal/reducer/ci_cd_run_correlation.go,
// ci_cd_run_correlation_workflow_image.go):
//
//   - Run (ci.run): one provider CI/CD run execution.
//   - Artifact (ci.artifact): one artifact emitted by a provider run.
//   - EnvironmentObservation (ci.environment_observation): one environment/
//     deployment-gate observation reported by a provider run or job.
//   - TriggerEdge (ci.trigger_edge): one explicit run trigger or upstream run
//     edge reported by the provider.
//   - Step (ci.step): one step under a provider job, read for its
//     deployment_hint_source shell-only signal.
//   - WorkflowImageEvidence (ci.workflow_image_evidence): one static workflow
//     command evidence row the git collector extracts from a checked-in
//     GitHub Actions workflow file (go/internal/collector/git_workflow_image_facts.go),
//     distinct from the ci_cd_run collector's provider-run facts above.
//
// Two emitted-but-unread-by-the-reducer kinds (ci.job, ci.pipeline_definition)
// and one warning kind (ci.warning) are intentionally NOT modeled here: no
// reducer or storage read path decodes their payload today
// (go/internal/reducer/ci_cd_run_correlation.go's cicdRunCorrelationFactKinds
// does not load ci.job or ci.pipeline_definition at all, and ci.warning is
// collected only as fixture provenance). Adding them is future follow-up work
// when a consumer needs them, matching how the sbom_attestation family left
// sbom.dependency_relationship/sbom.external_reference typed-but-deferred
// rather than typing kinds with no read site.
//
// Every struct here is FLAT: no ci_cd_run fact kind is a polymorphic
// multi-shape envelope, so none carries an untyped Attributes pass-through.
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming the
// field, never a zero-value struct. Optional fields are pointers or slices
// carrying omitempty, so an absent value decodes to nil and stays distinct
// from an observed zero.
//
// The reducer decodes only the latest struct for each kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
