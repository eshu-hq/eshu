# #6693 checklist step 4: `facts/payload/`

Baseline: `origin/main` `aa7cc0d1d`. Change: move the JSON payload codec and
empty-string SQL-binding helpers out of the `storage/postgres` root into a
new leaf package, `go/internal/storage/postgres/facts/payload` (package
`payloadstore`), per
[6693-postgres-target-tree/facts.md](../design/6693-postgres-target-tree/facts.md#factspayload-1-non-test-2-test).

## What moved

- `facts_payload.go` ->
  `go/internal/storage/postgres/facts/payload/json.go`.
- `facts_payload_test.go` ->
  `go/internal/storage/postgres/facts/payload/json_test.go` (in-package
  `payloadstore` test; it only exercised the moved codec, no root symbols).
- Exported the four functions every caller across the repo needs:
  `marshalPayload` -> `MarshalPayload`, `unmarshalPayload` ->
  `UnmarshalPayload`, `emptyToNil` -> `EmptyToNil`, `emptyToDefault` ->
  `EmptyToDefault`. `sanitizeJSONB` and `stripUnescapedJSONNulls` stay
  unexported: nothing outside `json.go` calls them.
- Added `doc.go`, `README.md`, `AGENTS.md` for the new package, modeled on
  `storage/postgres/semantic` and `storage/postgres/scope`.

## Mapping deviation: `proof_domain_terraform_support_test.go` stays in root

The mapping listed `proof_domain_terraform_support_test.go ->
facts/payload/proof_domain_terraform_support_test.go`, but the file's actual
symbol dependencies do not support that move:

- It defines `cloneEvidenceFacts`, `proofFactRows`,
  `proofRepositoryCatalogRows`, `proofLatestRelationshipFactRows`, and
  `proofFactEnvelopeRow` — test helpers consumed by three files that stay in
  root: `proof_domain_harness_test.go`, `proof_domain_tx_harness_test.go`,
  and `proof_domain_retirement_support_test.go`.
- It consumes `evidenceRecord` (defined in root's
  `relationship_store_test.go`) and `proofState` (defined in root's
  `proof_domain_harness_test.go`) — both unexported root test types.

Moving this file would require exporting two root-private test types plus
five test helper functions, and would make root test files import back into
`facts/payload` for helpers a leaf package should never own. The file's only
tie to `facts/payload` is calling `EmptyToDefault` three times, same as every
other root call site. Per the executor brief's re-derivation instruction, the
file stays at `go/internal/storage/postgres/proof_domain_terraform_support_test.go`
and its `EmptyToDefault` calls were repointed to `payloadstore.EmptyToDefault`
like any other root caller.

## Callers repointed

Production (root `postgres` package): `facts.go`, `facts_streaming.go`,
`collector_generation_dead_letter.go`, `relationship_reference_keys.go`,
`reducer_queue_helpers.go`, `relationship_evidence_batch.go`,
`projector_queue_scan.go`,
`facts_active_supply_chain_impact_identity_paging.go`, `reducer_queue.go`,
`ingestion_queries.go`, `relationship_store.go`.

Root tests: `proof_domain_tx_harness_test.go`,
`facts_documentation_ocr_test.go`,
`facts_documentation_google_workspace_test.go`,
`reducer_queue_payload_test.go`, `proof_domain_harness_test.go`,
`facts_documentation_structured_diagram_test.go`,
`proof_domain_terraform_support_test.go`.

Also fixed prose comments in other packages that named the old lowercase
symbol or the old file path so they no longer describe stale identifiers:
`internal/relationships/gcp_evidence.go`,
`internal/projector/decode/schema_version_admission.go`,
`internal/reducer/codedataflow_input_invalid_test.go`,
`internal/reducer/servicecatalog/service_catalog_persisted_version_test.go`,
`internal/reducer/code/call/persisted_version_test.go`,
`internal/reducer/schemadecode/factschema_decode.go`,
`internal/reducer/AGENTS.md`,
`sdk/go/factschema/codedataflow/v1/doc.go`,
`sdk/go/factschema/codedataflow/v1/README.md`.

Updated the two CI-gate trigger path lists in `specs/ci-gates.v1.yaml`
(ifa-determinism gate, both occurrences) and the ifa live-gate selector case
table in `scripts/lib/ifa_live_gate_selector_cases.sh` from
`facts_payload.go` to
`go/internal/storage/postgres/facts/payload/json.go` so the gate keeps
triggering on the moved file.

## dirgate

`bash scripts/verify-dirgate.sh --digest internal/storage/postgres` now
prints `count 361`, `digest 136a827c92e817e562c30d6ca279fabea1b57e8b73e105c66352177e6a30f016`.
Replaced the `internal/storage/postgres` row in
`scripts/lib/dirgate-grandfather.tsv` with that count and digest, then ran
`bash scripts/generate-dirgate-grandfather-go.sh` to regenerate
`tools/golangci-lint-dirgate/grandfather.go`.

No-Regression Evidence: this is a byte-identical move plus symbol export —
`MarshalPayload`, `UnmarshalPayload`, `EmptyToNil`, and `EmptyToDefault`
retain the exact bodies of `marshalPayload`, `unmarshalPayload`,
`emptyToNil`, and `emptyToDefault`. `go build ./...`, `go vet ./...`, and
`go test ./internal/storage/postgres/... -race -count=1` (including the new
`facts/payload` package) pass with no other production or test file
changed beyond the import/call-site repoint.

No-Observability-Change: no metric, span, log key, or status field is
added, removed, or renamed; this package has no telemetry of its own.
