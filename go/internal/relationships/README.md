# Relationships

## Purpose

`relationships` extracts IaC deployment evidence from fact envelopes and
resolves that evidence into typed cross-repository relationships before reducer
admission. It covers Terraform, Terraform provider schemas, Terragrunt, Helm,
Kustomize, Argo CD, GitHub Actions, Jenkins, Ansible, Dockerfile, and Docker
Compose source signals.

The package describes evidence rather than inventing deployment truth.
Extractors emit `EvidenceFact` values; the `Resolve` function promotes them to
`ResolvedRelationship` values only when confidence meets the threshold and no
rejection assertion exists. Ambiguous signals stay ambiguous until a stronger
contract — such as an explicit `Assertion` — admits them.

## Where this fits in the pipeline

```mermaid
flowchart LR
  A["Postgres fact store\nfacts.Envelope slice"] --> B["DiscoverEvidence\n(evidence.go)"]
  B --> C["EvidenceFact slice"]
  C --> D["Resolve\n(resolver.go)"]
  D --> E["ResolvedRelationship slice"]
  E --> F["Reducer\nrelationship_evidence_facts\nresolved_relationships"]
```

`DiscoverEvidence` and `Resolve` are called from `internal/reducer` during the
relationship evidence domain pass, not from the projector.

## Internal flow

```mermaid
flowchart TB
  A["DiscoverEvidence\nenvelopes + catalog"] --> B["buildEvidenceContentIndex\nrepo→files map"]
  B --> C["discoverFromEnvelopeWithIndex\nper envelope"]
  C --> D{"artifact_type\nor path"}
  D -- terraform/hcl --> E["discoverTerraformEvidence\n+ discoverTerraformSchemaEvidence"]
  D -- helm --> F["discoverHelmEvidence"]
  D -- kustomize --> G["discoverKustomizeEvidence"]
  D -- argocd --> H["discoverArgoCDEvidence"]
  D -- github_actions --> I["discoverGitHubActionsEvidence"]
  D -- jenkins --> J["discoverJenkinsEvidence"]
  D -- ansible --> K["discoverAnsibleEvidence"]
  D -- dockerfile --> L["discoverDockerfileEvidence"]
  D -- docker_compose --> M["discoverDockerComposeEvidence"]
  E & F & G & H & I & J & K & L & M --> N["matchCatalog\nCatalogEntry.Aliases"]
  N --> O["EvidenceFact slice"]
  O --> P["Resolve\nbuildCandidates → groupAssertions → filter"]
  P --> Q["[]Candidate\n[]ResolvedRelationship"]
```

## Lifecycle / workflow

`DiscoverEvidence` receives a slice of `facts.Envelope` values and a
`CatalogEntry` slice that maps repository IDs to known aliases. It calls
`buildEvidenceContentIndex` to build a `map[repoID][]file` index used by
template-driven ArgoCD extractors. It then routes each envelope to one or more
extractors based on `artifact_type` and file path heuristics. Every extractor
calls `matchCatalog`, which compares candidate strings against each
`CatalogEntry.Aliases` entry using case-insensitive substring matching. A
per-call `seen` map deduplicates facts within a single pass.

Argo CD Application evidence accepts the legacy singular `source_repo` field and
the multi-source `source_repos` field emitted for `spec.sources`. Each matched
source repository produces its own `EvidenceKindArgoCDAppSource` fact. The
ApplicationSet extractor keeps generator discovery sources separate from
template deploy sources so the reducer can preserve discovery versus deployment
intent.

Schema-driven Terraform extraction (`discoverTerraformSchemaEvidence`) uses
`RegisterSchemaDrivenTerraformExtractors` to bootstrap extractors from
packaged provider schemas at first call. Each schema-derived extractor runs
`InferIdentityKeys` on the resource attributes to pick a stable candidate name
key, then calls `matchCatalog` with confidence derived from whether a known
identity key was found (`0.78`) or only the resource block name was available
(`0.55`).

`Resolve` groups `EvidenceFact` values into `Candidate` buckets by
`(SourceEntityID, TargetEntityID, RelationshipType)`, applies rejection and
explicit assertion overrides from `Assertion` values, filters by
`confidenceThreshold` (default `DefaultConfidenceThreshold` = 0.75), and
returns both the candidate list and the promoted `ResolvedRelationship` slice.

## Exported surface

- `DiscoverEvidence(envelopes, catalog)` — scan envelopes for IaC evidence;
  returns a deduplicated `[]EvidenceFact` (`evidence.go:18`)
- `Resolve(evidenceFacts, assertions, confidenceThreshold)` — group evidence
  into `[]Candidate`, apply `Assertion` overrides, filter by confidence, and
  return both slices (`resolver.go:62`)
- `DedupeEvidenceFacts(facts)` — collapse exact-duplicate `EvidenceFact`
  values while preserving discovery order (`resolver.go:16`)
- `ResolvedRelationshipID(generationID, r, ordinal)` — build the durable
  Postgres identity for a resolved relationship (`models.go:163`)
- `RegisterSchemaDrivenTerraformExtractors(schemaDir)` — bootstrap schema-
  driven Terraform resource extractors from a provider schema directory;
  returns a `map[string]int` summarizing registered resource types per
  provider (`terraform_schema.go:49`)
- `DefaultConfidenceThreshold` — 0.75; minimum confidence to promote an
  inferred candidate to a resolved relationship (`resolver.go:12`)

### Core types

- `EvidenceFact` — one raw observed signal: `EvidenceKind`, `RelationshipType`,
  `SourceRepoID`, `TargetRepoID`, `Confidence`, `Rationale`, `Details`
  (`models.go:109`)
- `EvidenceKind` — string enum of evidence origins: `EvidenceKindTerraformAppRepo`,
  `EvidenceKindTerraformModuleSource`, `EvidenceKindHelmChart`,
  `EvidenceKindArgoCDAppSource`, `EvidenceKindGitHubActionsReusableWorkflow`,
  `EvidenceKindJenkinsSharedLibrary`, `EvidenceKindAnsibleRoleReference`,
  and 20+ others (`models.go:13`)
- `RelationshipType` — string enum of edge semantics: `RelDeploysFrom`,
  `RelUsesModule`, `RelProvisionsDependencyFor`, `RelDiscoversConfigIn`,
  `RelReadsConfigFrom`, `RelRunsOn`, `RelDependsOn` (`models.go:79`)
- `Candidate` — aggregated machine-generated relationship with combined
  `Confidence`, `EvidenceCount`, and merged `Details` (`models.go:134`)
- `ResolvedRelationship` — canonical relationship emitted after resolution;
  carries `ResolutionSource` (`inferred` or `assertion`) (`models.go:147`)
- `Assertion` — explicit human or control-plane override with `Decision`
  `"assert"` or `"reject"` (`models.go:122`)
- `CatalogEntry` — maps one `RepoID` to its `Aliases` slice used by
  `matchCatalog` (`evidence.go:11`)
- `Generation` — lifecycle record for one resolution run (`models.go:171`)

## Dependencies

- `internal/facts` — `facts.Envelope`; the durable fact model this package
  reads. The envelope's `Payload` map carries `artifact_type`, `relative_path`,
  `content`, `repo_id`, and `parsed_file_data`.
- `internal/terraformschema` — `terraformschema.LoadProviderSchema`,
  `terraformschema.InferIdentityKeys`, `terraformschema.ClassifyResourceCategory`;
  consumed by `RegisterSchemaDrivenTerraformExtractors` and the Terraform
  schema extractor path in `terraform_schema.go`.

Reducer admission lives in `internal/reducer`. This package supplies evidence
and resolved relationships; it never writes to the graph or queue directly.

## Telemetry

This package does not emit its own metrics, spans, or structured logs.
Extraction outcomes are surfaced by the reducer when admitted and by
`internal/storage/postgres` when persisted as evidence rows.

## Operational notes

- If relationship evidence is sparse for a repository, check that its
  `CatalogEntry.Aliases` includes the names actually referenced in IaC files
  (repo short name, org/repo form, and any known aliases). `matchCatalog` uses
  case-insensitive substring matching; overly short aliases can match
  unrelated candidates.
- `RegisterSchemaDrivenTerraformExtractors` is called lazily on first
  `discoverTerraformSchemaEvidence` call. If the schema directory is missing
  or empty, schema-driven extraction silently produces no evidence. Call the
  function explicitly during process startup to surface schema loading errors
  early.
- ArgoCD ApplicationSet template evaluation (`argocd_template_params.go`)
  requires that generator config files exist in the same envelope batch passed
  to `DiscoverEvidence`. Template parameters not present in the content index
  will leave the template unresolved and no evidence will be emitted for those
  dynamic sources.
- Confidence thresholds in `Resolve` are applied to the maximum confidence
  across the evidence bucket, not the average. A single high-confidence signal
  is sufficient to promote a candidate.

## Extension points

- **Add a new extractor** — add a new `discover*Evidence` function following
  the existing pattern, wire it into `discoverFromEnvelopeWithIndex` in
  `evidence.go`, add a corresponding `EvidenceKind` constant to `models.go`,
  and add a test file. The extractor must call `matchCatalog` and respect the
  `seen` deduplication map.
- **Add a new relationship type** — add a `RelationshipType` constant to
  `models.go` and use it in the appropriate extractor. Document the admission
  semantics if the reducer needs a new domain for this edge kind.
- **Add a new Terraform resource extractor** — add a provider schema `.json.gz`
  to `internal/terraformschema/schemas/` and regenerate or call
  `RegisterSchemaDrivenTerraformExtractors` with the updated directory.

## Gotchas / invariants

- Extractors must be deterministic over the same input bytes. Repeated runs
  over the same snapshot must produce the same `EvidenceFact` set (`doc.go`).
- `Resolve` deduplicates both the candidate and the resolved output. Do not
  pre-deduplicate evidence before calling `Resolve`; the deduplication inside
  `buildCandidates` relies on seeing the full evidence set to compute
  `EvidenceCount` correctly.
- `Assertion.Decision` must be exactly `"assert"` or `"reject"`. Values that
  do not match either string are silently ignored by `groupAssertions`
  (`resolver.go:220`).
- Terraform registry sources (three-part `namespace/provider/type` form) are
  explicitly excluded from module-source evidence because they reference a
  public registry module, not a repository alias.
- Terragrunt helper function calls such as `get_repo_root()` and
  `path_relative_to_include()` are stripped by
  `normalizeTerraformEvidencePathExpression` before alias matching. Paths that
  contain unsupported helper expressions produce no evidence.

## Related docs

- `docs/docs/architecture.md` — ownership table and pipeline overview
- `docs/docs/reference/local-testing.md` — verification gates
- `go/internal/terraformschema/README.md` — provider schema loader details
- `go/internal/iacreachability/README.md` — complement: reachability analysis
