# SLSA Provenance Backbone Evidence (#5456)

Extends `attestation.slsa_provenance` with `materials[]{uri,digest}` +
`invocation.configSource`, retains the in-toto predicate body on collection, and
adds a container-image identity source-revision tier `slsa_provenance_commit`
that outranks `oci_config_source_label` and `ci_run_commit`. #5371 already
landed the producer/consumer/read-surface; this extends them.

## No-Regression Evidence

No-Regression Evidence: the SLSA identity-ranking and predicate-retention paths
add no new query, Cypher, worker, lease, queue, or batch — an in-memory
digest→commit map join mirroring the shipped ci-run anchor; B-7 golden gate
~33s (green) vs ~34s #5459 baseline on the same 20-repo corpus + pinned
NornicDB v1.1.11/Postgres, `list_sbom_attestation_attachments` result count and
`container_image_identity` decision cardinality unchanged (details below).

This change touches reducer and collector files
(`go/internal/collector/sbomruntime/attestation*.go`,
`go/internal/reducer/container_image_identity*.go`,
`go/internal/reducer/sbom_attestation_attachment*.go`) but adds no new database
query, Cypher statement, worker, lease, queue, batch, or concurrency knob. The
touched paths stay on their existing cost profile:

- **Identity ranking** — `applySLSADigestRevision`
  (`go/internal/reducer/container_image_identity_slsa.go`) builds an in-process
  `map[digest]commit` anchor from the `attestation.slsa_provenance` facts already
  loaded for the scope generation, then does an O(1) map lookup per container-image
  ref, exactly mirroring the shipped `applyCIRunDigestRevision` /
  `ciRunDigestAnchor` path. No new fact load, no new scan, no extra DB round-trip;
  the decision cardinality (`eshu_dp_container_image_identity_decisions_total`)
  is unchanged.
- **Predicate retention** — `sbomruntime/attestation_slsa.go` decodes
  `materials`/`configSource` out of the in-toto predicate body that was already
  `json.Unmarshal`'d for `builder.id`; it adds field extraction, not a new pass
  over new input. `materials[]` is write-capped at
  `maxSBOMAttachmentSLSAMaterialRows = 20` with a `material_count` +
  `materials_truncated` pair, so the read surface stays bounded.
- **Schema** — additive-optional `materials`/`config_source` fields are a minor
  (`1.0.0`→`1.1.0`) bump per contract-rigor; `factschema-diff` confirms
  `attestation.slsa_provenance.v1.schema.json — no breaking changes`. Old
  `1.0.0` facts still admit.

**Baseline vs after (same input shape / corpus / storage state):** the B-7 golden
corpus gate over the 20-repo fixture with the sbom_attestation cassette (1 SLSA
statement carrying synthetic `materials` + `configSource`), on the pinned
NornicDB (v1.1.11) + Postgres backends, `graph/query/timing` phase:

| Metric | Before (#5459 baseline, same corpus) | After (#5456) | Input shape / corpus |
| ------ | ------------------------------------ | ------------- | -------------------- |
| golden-corpus gate wall time | ~34s (green) | ~33s (green) | 20-repo B-7 corpus, remapped-port compose |
| `list_sbom_attestation_attachments` result count | 2 | 2 | subject_digest=sha256:2b3c… |
| container_image_identity decisions | unchanged | unchanged | SLSA subject digest matches no corpus image_manifest digest (see scope note) |

Terminal state: `PASS: B-7 golden corpus gate green`, reducer drain to zero, no
new required-fail. The read surface returns `slsa_provenance_materials`,
`slsa_provenance_config_source_uri/entry_point/digest` end-to-end (asserted with
values in the B-12 snapshot). Focused suites green: `internal/reducer`,
`internal/collector/sbomruntime`, `internal/query`, `internal/mcp`,
`cmd/golden-corpus-gate`, `sdk/go/factschema/...`. Identity-ranking behavior is
proven by the failing-then-green 6-case proof matrix
(`container_image_identity_slsa_test.go`) against real
`BuildContainerImageIdentityDecisions`.

## No-Observability-Change

No-Observability-Change: no new metric or span; the touched stages stay covered
by `eshu_dp_container_image_identity_decisions_total`,
`eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds`, the
`eshu_dp_reducer_input_invalid_facts_total` quarantine, and the existing
collector observe/commit signals (see the telemetry-coverage rows added in this
PR).

No new metric or span. The touched stages remain covered by existing signals:
container-image identity decisions by
`eshu_dp_container_image_identity_decisions_total` plus
`eshu_dp_reducer_executions_total` / `eshu_dp_reducer_run_duration_seconds`;
malformed SLSA evidence by the `eshu_dp_reducer_input_invalid_facts_total`
quarantine (`domain`=`container_image_identity`); SBOM attestation collection by
the existing collector observe/commit signals. The two new stage files are
recorded with `No-Observability-Change:` rows in
`docs/public/observability/telemetry-coverage.md`.

## Scope note

The 20-repo corpus's SBOM subject digest does not match any collected
`oci_registry.image_manifest` digest, so the cassette exercises the read surface
(materials/config_source, asserted with values) but not an identity-decision
flip to `slsa_provenance_commit`. That flip is proven by the unit proof matrix,
consistent with the epic's framework-proof-before-corpus rule.
