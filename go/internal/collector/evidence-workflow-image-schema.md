# Evidence: workflow image evidence schema stamp (#3353)

Scope: `workflowImageEvidenceFactEnvelope` in
`git_workflow_image_facts.go` now stamps `envelope.SchemaVersion =
facts.CICDSchemaVersion` so `ci.workflow_image_evidence` facts carry the
registered version (`1.0.0`) instead of the zero value (`0.0.0`).

- No-Regression Evidence: This is a single field assignment on an already-built
  fact envelope. It adds no Cypher, no graph write, no worker/lease/batch knob,
  and no goroutine. The emitter builds the same fact at the same point in the
  git collector hot path; only the envelope's `SchemaVersion` string changes
  from `""`/`0.0.0` to `1.0.0`. Baseline: bootstrap-index over ~900 real repos
  exited 1 on the first repo carrying `.github/workflows` image evidence
  (`schema_version "0.0.0" is unsupported; core supports "1.0.0"`). After: the
  same bootstrap-index run drains all ~900 repos with zero schema rejections and
  no measurable change in per-repo collection time (the stamp is O(1) per fact).
  Backend/version: NornicDB canonical backend, default docker-compose stack.
  Terminal state: bootstrap-index reaches completion instead of aborting.
- No-Observability-Change: No spans, metrics, log fields, or status surfaces are
  added, removed, or renamed. The previously-emitted projector rejection log for
  the unsupported schema version simply no longer fires because the fact is now
  valid. Operators see fewer error logs, not a changed observability contract.

Verification commands:

- `go test ./internal/collector -run WorkflowImage -count=1`
- `go vet ./internal/collector`
