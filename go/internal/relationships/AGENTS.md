# AGENTS.md — internal/relationships guidance for LLM assistants

## Read first

1. `go/internal/relationships/README.md` — pipeline position, extractor
   inventory, lifecycle, and invariants
2. `go/internal/relationships/evidence.go` — `DiscoverEvidence` and
   `discoverFromEnvelopeWithIndex`; understand the routing logic before
   adding a new extractor
3. `go/internal/relationships/models.go` — all exported types and constants;
   every new extractor must reference types defined here
4. `go/internal/relationships/resolver.go` — `Resolve`, `buildCandidates`,
   `groupAssertions`; understand candidate aggregation before changing
   confidence math
5. `go/internal/relationships/terraform_schema.go` — schema-driven extractor
   bootstrap; read before touching Terraform resource extraction
6. `go/internal/terraformschema/README.md` — provider schema loader that
   `RegisterSchemaDrivenTerraformExtractors` depends on

## Invariants this package enforces

- **Determinism** — `DiscoverEvidence` must produce the same `EvidenceFact`
  set over the same input bytes. Do not use maps to drive iteration order in
  extractors, and do not read from non-deterministic sources.
- **Ambiguity preservation** — if an extractor cannot determine a concrete
  target, it must produce no evidence rather than a speculative match.
  Ambiguous signals stay ambiguous until an explicit `Assertion` admits them.
- **No graph writes** — this package never calls the graph backend or the
  reducer queue. Evidence output is consumed by `internal/reducer`.
- **Seen-map deduplication** — every extractor receives the per-discovery-pass
  `seen map[evidenceKey]struct{}` and must check it before appending. Do not
  bypass this check; duplicate facts distort `EvidenceCount` in `Candidate`.
- **Confidence is max, not average** — `aggregateCandidate` in `resolver.go`
  takes the maximum confidence across the evidence bucket. Picking the right
  per-fact confidence for a new extractor is the main tuning dial.

## Common changes and how to scope them

- **Add a new IaC extractor** → add a `discover*Evidence` function, wire it
  into the `discoverFromEnvelopeWithIndex` switch in `evidence.go`, add a new
  `EvidenceKind` constant to `models.go`, add a test file named
  `<tool>_evidence_test.go`. Run `go test ./internal/relationships -count=1`.
  Why: the routing switch is the only place artifact type dispatch lives; a
  new extractor wired nowhere produces no evidence silently.

- **Add a new relationship type** → add a `RelationshipType` constant to
  `models.go`, use it in the extractor, and document whether the reducer
  needs a new domain for this edge kind. Check `internal/reducer` domain
  constants before inventing a new edge semantic.

- **Change confidence values** → confidence affects `DefaultConfidenceThreshold`
  filtering in `Resolve`. Before lowering a value below 0.75, verify that the
  reducer admission contract for the affected domain still holds. Raise a
  threshold only with evidence that it eliminates false positives.

- **Add a Terraform resource type extractor** → add the provider schema `.json.gz`
  to `go/internal/terraformschema/schemas/` (see `schemas/README.md`), then
  call `RegisterSchemaDrivenTerraformExtractors` or let the lazy bootstrap
  pick it up. Add a test case in `terraform_schema_test.go`.

- **Change ArgoCD template evaluation** → `argocd_template_params.go` handles
  flat YAML param resolution. Template parameters that reference Go template
  syntax (`{{ .path.basename }}`) are evaluated statically from the content
  index. Do not add dynamic execution; static flattening is intentional.

## Failure modes and how to debug

- Symptom: no evidence emitted for a known IaC file →
  likely cause: `artifact_type` in the envelope does not match extractor
  routing → print `artifact_type` from the envelope payload; check the
  `discoverFromEnvelopeWithIndex` switch; verify the `isXxxArtifact` predicate
  for the format in question.

- Symptom: evidence emitted but relationship not resolved →
  likely cause: confidence below `DefaultConfidenceThreshold` or a `reject`
  assertion is present → inspect the `Candidate` slice returned by `Resolve`;
  check `Candidate.Confidence` against the threshold; check whether a
  `reject` assertion exists for the pair.

- Symptom: schema-driven Terraform evidence missing →
  likely cause: `RegisterSchemaDrivenTerraformExtractors` was not called or
  the schema directory is empty → check that `terraformschema.DefaultSchemaDir()`
  returns a non-empty path; call `RegisterSchemaDrivenTerraformExtractors`
  explicitly at startup and log the returned summary.

- Symptom: ArgoCD ApplicationSet produces no deploy-source evidence →
  likely cause: the config generator files were not included in the envelope
  batch → the content index is built per `DiscoverEvidence` call from the
  supplied envelopes; ensure generator config files for the ApplicationSet
  repository are included.

## Anti-patterns specific to this package

- **Inventing deployment truth** — do not synthesize relationships from
  heuristics that do not trace back to a concrete file reference. Evidence must
  have a real file path, matched alias, and rationale.
- **Writing to the graph or queue** — this package has no graph or queue
  dependency. Any import of `internal/storage` or `internal/reducer` queue
  types is a boundary violation.
- **Bypassing `matchCatalog`** — all catalog-matching must go through
  `matchCatalog`. Ad-hoc alias matching outside that function breaks
  deduplication and rationale recording.
- **Lowering the confidence threshold at the extractor level** — if a signal
  is genuinely uncertain, keep its confidence low and let the reducer or an
  assertion decide rather than inflating confidence to pass the filter.

## What NOT to change without an ADR

- `DefaultConfidenceThreshold` — the 0.75 default is relied on by reducer
  admission tests; changing it affects which relationships materialize in
  production graphs.
- The `EvidenceKind` string values once used in persisted fact rows — renames
  require a data migration; check `internal/storage/postgres` before renaming.
- The `Resolve` assertion contract (`"assert"` / `"reject"` decision strings)
  — these are used in control-plane APIs; changing them breaks existing
  assertions stored in Postgres.
