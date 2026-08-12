# AGENTS — cloudruntime

## Read first

1. `doc.go` — package contract.
2. `classify.go` — exclusive existence dispatch plus value-drift comparison
   (`ClassifyValueComparison`, `ClassifyValueDrift`,
   `ClassifyContainerImageDrift`, #5453/#5837).
3. `value_attribute_allowlist.go` — the bounded comparable-attribute allowlist.
4. `container_image_extract.go` — SECURITY-BOUNDED ECS container-image
   extraction; read this before touching anything ECS-shaped.
5. `candidate.go` — ARN-keyed candidate and evidence construction.
6. `telemetry.go` — bounded metric emission.
7. `../../rules/aws_cloud_runtime_drift_rules.go` — rule-pack declaration.
8. `docs/public/reference/relationship-mapping.md`
   — cloud observation joins phase.

## Invariants

- `Classify` returns at most one `FindingKind` per ARN.
- Cloud-only resources are orphaned before unmanaged can be considered.
- Unmanaged requires AWS cloud and Terraform state evidence but no current
  Terraform config evidence.
- `BuildCandidates` must emit an `EvidenceTypeCloudResourceARN` atom or the
  rule pack's structural gate rejects the candidate.
- Raw tag evidence stays in `EvidenceAtom`s only. Do not add tag keys, tag
  values, ARNs, Terraform addresses, or account IDs as metric labels.
- `ClassifyValueDrift` only fires once cloud, state, AND config all agree the
  resource is Terraform-managed (existence findings always take precedence).
- **An empty `Classify` return means CONVERGENCE, and callers act on it
  destructively.** `BuildCandidates` drops the ARN, so the reducer's
  generation-authoritative retire deletes whatever finding the ARN held. Never
  return `""` for a case that means "I could not tell" — that is
  `FindingKindValueComparisonInconclusive` (#5837). Before adding any new
  early-return to `Classify`, decide which of the two it is.
- `ValueComparison.Inconclusive()` is the only condition permitted to produce
  that kind: the resource type IS covered (`Comparable` non-empty) and NOT ONE
  comparison ran (`Compared` empty) OR a covered comparable was UNREADABLE
  (`Degraded` non-empty). Never widen it to "some comparison failed": absence
  is not degradation, and that widening would put a finding on every
  zip-packaged Lambda. The readable-versus-unreadable call is the LOADER's —
  this package only consumes `ResourceRow.DegradedAttributes` — see the
  README's unreadable-versus-absent section (#5859/#5904 for redaction, #5861
  for an unobservable `image_uri` and the per-attribute rule).
- `ContainerImageExtractionResult.Degraded` means the source carried a value
  that could not be read; absent evidence leaves it false. Do not collapse the
  two — the loaders raise `container_images_unreadable` off it.
- `ExtractDeclaredContainerImages`/`ExtractObservedContainerImages` return
  ONLY the bounded `image` field of each container, capped at
  `MaxContainerImagesPerResource`. NEVER add a field to
  `declaredContainerDefinition` or read a second key off the observed
  container map -- `container_definitions` and the AWS `containers`
  attribute both carry `environment`/`secrets`.

## Common changes

- Add a finding kind: update `FindingKind`, `Classify`, `BuildCandidates`
  evidence, `managementStatusForFinding`, `RecordEvaluation` (both here and in
  `../multicloud`), the reducer summary line, the query package's
  `normalizeIaCManagementFindingKinds` allowlist, the MCP/OpenAPI descriptions,
  tests, telemetry docs, and the active ADR.
- Change candidate evidence shape: keep `EvidenceTypeCloudResourceARN` aligned
  with `rules.AWSCloudRuntimeDriftRulePack`.
- Add reducer wiring: keep loaders and graph publication outside this package.
  This package should stay a deterministic classifier and candidate builder.

## Failure modes

- No orphan/unmanaged counters: check that callers ran `engine.Evaluate` with
  `rules.AWSCloudRuntimeDriftRulePack()` and then called `RecordEvaluation`.
- Structural mismatches: check for a missing `aws_cloud_resource_arn` evidence
  atom.
- False unmanaged findings: verify the loader joined current Terraform config
  by ARN before constructing `AddressedRow`.

## Anti-patterns

- Do not query Postgres or graph backends from this package.
- Do not infer environment or service ownership from tag names here. Tags are
  raw evidence for a later normalization rule.
- Do not add backend-specific NornicDB or Neo4j branches.
- Do not widen `declaredContainerDefinition` or the observed-container map
  read past `image`. Do not raise `MaxContainerImagesPerResource` without a
  fresh review of the bounded-evidence rationale in `container_image_extract.go`.
- Do not add `aws_db_instance`/generic `engine_version` to
  `valueAttributeAllowlist` until the AWS collector emits an observed
  `engine_version` value -- see the README's bounded-gap section.

## What NOT to change without an ADR

- The ARN-primary join contract.
- The exclusive orphan-before-unmanaged dispatch order.
- The metric label set for orphan and unmanaged counters.
- The existence-findings-before-value-drift precedence in `Classify`.
- The convergence-versus-inconclusive split, which the retire's correctness
  depends on (#5837).
- `ValueComparisonGapAttributes` — the kind-to-attribute rule for
  `missing_evidence`. The multicloud candidate builder calls it too; a second
  copy of that switch lets the two routes describe the same finding
  differently (#5861).
