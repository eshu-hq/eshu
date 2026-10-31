# Evidence: #8878 in-tree scanner removal

## No-Regression Evidence

The removal deletes the execution half and keeps every decision surface
proved:

- Frozen vocabulary: `go/internal/collector/scannercomponent/vocabulary_test.go`
  asserts every `AnalyzerKind`, `TargetKind`, `FailureClass`,
  `FailureDisposition`, `ExecutionLane`, component ID, manifest version,
  protocol string, and artifact digest pin verbatim.
- Component admission: the moved `manifest_test.go` and `reconcile_test.go`
  still prove fails-closed pins, per-analyzer identities, grant coverage,
  tampered/missing/unknown artifact denial, and non-scanner passthrough.
- Paging and host admission: `extensionhost` golden parity tests feed
  committed analyzer-family envelopes (`testdata/scanner-parity/`) through
  the host at a small page bound — 7/13/12 pages reassemble with identical
  kind, stable key, generation, and scope, and every page is admitted.
- Planner: `go/internal/coordinator/scanner/worker` tests prove deterministic
  target derivation against the leaf vocabulary.
- Seeded admission: `seeded_artifact_test.go`, `seed_pin_admission_test.go`,
  and the Helm activations contract test repoint at the leaf with unchanged
  assertions.
- Reducer attachment: satellite SBOM envelopes built through the SDK
  factschema sbom/v1 encoders prove `attached_parse_only`,
  `unknown_subject`, lockfile evidence, and aggregate warning counts with
  unchanged expected values.
- Terminal failures: the status AST check now declares `FailureClass`
  against `collector/scannercomponent`.
- Registry: `scripts/verify-fact-kind-registry.sh` passes after regenerating
  the three scanner authority rows to the satellite repository.
- Contract gates: `scripts/verify-factschema-diff.sh` and
  `scripts/verify-payload-usage-manifest.sh` pass — no payload shape changed.
- Digest pins: `scripts/test-verify-scanner-digest-pins.sh` passes 29/29
  (seeded typo/404/attestation RED cases, fail-closed anchor cases).

## Observability Evidence

No-Observability-Change with one ownership move: the
`eshu_dp_scanner_worker_*` instruments and span names stay registered in
`go/internal/telemetry/instruments.go`; the satellite emits its own copies
with the frozen analyzer, target-kind, and failure-class label values pinned
in `vocabulary.go`. `scripts/verify-telemetry-coverage.sh` agrees docs and
registrations with no new untracked stages. An operator tracing a scanner
dead-letter at 3 AM reads the same `failure_class` values in the status
surface as the satellite emits, because both key on the frozen vocabulary.
