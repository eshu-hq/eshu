# ADR: OCI Container Registry Collector

**Date:** 2026-05-10
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-04-20-aws-cloud-scanner-collector.md`
- `2026-05-12-package-registry-collector.md`
- `2026-05-09-optional-component-boundary.md`
- Issue: `#15`
- Issue: `#24`

---

## Context

Eshu already sees container image strings in Git, CI, Kubernetes manifests, and
AWS runtime evidence. Those strings are not enough. Tags are mutable, registries
can serve multi-platform image indexes, and supply-chain evidence such as SBOMs,
signatures, and attestations hangs off digest-addressed registry objects.

The OCI registry collector adds registry-native image truth. It should observe
repositories, tags, manifests, image indexes, descriptors, and referrers from
OCI-compatible registries. Reducers then use digest-anchored facts to join Git,
CI, AWS, Kubernetes live, SBOM, vulnerability, and attestation evidence.

This collector belongs in the runtime, artifact, and security-intelligence wave
after Terraform state and AWS. It stays optional unless an operator configures
registry instances.

## Source Contracts

The first contract is OCI-first, not provider-first:

- The OCI Distribution API is the baseline source of truth.
- Provider adapters may exist for ECR, GHCR, Docker Hub, Harbor, Google
  Artifact Registry, Azure Container Registry, and Artifactory.
- Manifest digest is stable identity.
- Tags are observations of `tag -> digest` at a point in time.
- Image indexes and platform manifests are separate objects.
- Descriptors preserve media type, digest, size, annotations, and artifact
  type when present.
- Referrers are optional registry capabilities. Missing Referrers API support
  is a warning, not proof that no SBOMs, signatures, or attestations exist.
- ECR and JFrog Artifactory are the first live-validation adapters because
  they are available for direct testing. ECR stays in this OCI lane. JFrog also
  participates in the package-registry lane when collecting npm, Maven, NuGet,
  PyPI, Go, Generic, or other package-manager feed metadata.

## Initial Provider Support Matrix

The implementation starts with an OCI-first core and provider adapters only
where the provider changes authentication or URL shape.

| Provider | Initial status | Contract |
| --- | --- | --- |
| OCI Distribution-compatible registry | Supported baseline | `distribution.Client` performs `/v2/`, tag list, manifest, and Referrers API calls. |
| JFrog Artifactory Docker/OCI repository | Supported live-validation target | `jfrog` maps an Artifactory base URL plus Docker repository key onto the Distribution client. Package feeds remain in the package-registry collector. |
| Amazon ECR private registry | Supported live-validation target | `ecr` maps account/region registry hosts and converts `GetAuthorizationToken` output into Distribution basic auth. |
| Docker Hub | Supported live-validation target | `dockerhub` maps `docker.io` identity to `registry-1.docker.io`, adds `library/` for official images, and obtains pull tokens from Docker's token service. |
| GHCR | Supported live-validation target | `ghcr` validates owner/image names and obtains repository-scoped pull tokens from GitHub Container Registry. |
| Harbor, Google Artifact Registry, Azure Container Registry | Future adapters | Add only when a provider needs auth, pagination, hostname, or warning-class behavior beyond the Distribution baseline. |

Provider adapters must not change fact identity. They can normalize endpoint
shape, obtain credentials, classify provider-specific warnings, and pass the
result into the same `oci_registry` fact builders.

## Decision

Add a future collector family named `oci_registry`.

The collector owns:

- registry and repository discovery for configured instances
- tag listing
- manifest and image-index retrieval
- descriptor normalization
- referrer listing where available
- provider-auth failure classification
- typed fact emission

The collector does not own:

- canonical graph writes
- image-to-workload correlation
- vulnerability severity decisions
- SBOM parsing
- signature verification policy
- answer shaping

## Scope And Generation

Collector instance:

- one configured registry access boundary
- provider adapter and credential reference
- allowlisted repositories or repository prefixes
- optional rate and concurrency limits

Scope:

```text
scope_kind = container_registry_repository
scope_id   = oci-registry://<registry>/<repository>
```

Generation:

- coordinator-assigned monotonic generation per repository scan
- registry cursors or pagination state are checkpoint metadata
- manifest digest remains the stable object identity
- tag observations carry `observed_at` and previous digest when known

## Fact Families

Initial fact kinds:

| Fact kind | Purpose |
| --- | --- |
| `oci_registry_repository` | Registry host, repository name, provider adapter, visibility/auth mode when known, and scan policy. |
| `oci_image_tag_observation` | Tag, resolved digest, media type, observed time, previous digest when known, and mutation flag. |
| `oci_image_manifest` | Manifest digest, media type, size, config descriptor, layers, subject descriptor, and annotations. |
| `oci_image_index` | Index digest, media type, platform manifest descriptors, nested index descriptors, subject descriptor, and annotations. |
| `oci_image_descriptor` | Reusable descriptor fact for unknown or non-image media types that still need digest, size, annotations, and artifact type preserved. |
| `oci_image_referrer` | Subject digest, referrer digest, artifact type, media type, size, annotations, and source API path. |
| `oci_registry_warning` | Auth denied, rate limited, unsupported Referrers API, manifest unknown, digest mismatch, unknown media type, repository unavailable, and tag mutation detected. |

Every fact must carry:

- `collector_kind=oci_registry`
- `collector_instance_id`
- `scope_id`
- `generation_id`
- `source_confidence=reported`
- `fence_token`
- `correlation_anchors`

## Identity And Correlation Rules

Digest identity wins:

1. Manifest digest.
2. Image-index digest plus platform manifest digest.
3. Repository plus digest.
4. Repository plus tag as a weak observation.

The DSL must never treat `latest` or any other tag as immutable truth. A tag can
help explain what a runtime referenced, but canonical image identity is a
digest.

Runtime image references should join in this order:

1. Explicit image digest.
2. Registry tag observation resolving the tag to a digest in the same time
   window.
3. OCI annotations such as `org.opencontainers.image.source` and
   `org.opencontainers.image.revision`.
4. Repository+tag fallback with low confidence and conflict visibility.

## Referrers, SBOMs, And Attestations

The collector should preserve referrers as registry facts and not interpret
their security meaning.

Examples:

- SBOM descriptors
- signature descriptors
- provenance attestations
- vulnerability scan artifacts
- unknown artifact media types

Security interpretation belongs to later SBOM, attestation, and vulnerability
consumers. The registry collector only proves that the registry reported an
artifact connected to a subject digest.

If the registry does not support the Referrers API, the collector emits
`oci_registry_warning{kind=unsupported_referrers_api}`. It must not report
"no referrers" unless the API was supported and returned an empty result.

## Redaction And Security

The collector must avoid leaking private image topology through telemetry:

- image names and repository paths are not metric labels
- private registry hostnames are not metric labels unless explicitly allowed
- credentials never appear in logs, facts, traces, or warnings
- registry auth failures include provider, failure class, and credential ref
  hash only

Annotations are metadata, not automatically safe. The collector preserves known
OCI source/revision/version/base annotations and redacts unknown annotation
values by default unless an operator allowlist enables them.

## Reducer And Query Contracts

Projector/reducer ownership:

- A registry projector may materialize `(:ContainerImage)` by manifest digest.
- A registry projector may materialize `(:ContainerImageIndex)` by image-index
  digest.
- DSL joins connect registry facts to Git, CI, AWS, Kubernetes live, SBOM,
  vulnerability, and attestation facts.
- Query paths can answer digest evidence only after registry projection is
  ready for the repository scope.

Required query capabilities after implementation:

- Which workloads reference digest `sha256:...`?
- Which tags currently point to this digest?
- Did a tag move between generations?
- Which source repo or revision is declared by OCI annotations?
- Which images have SBOM or signature referrers?
- Which runtime image references lack registry evidence?

## Operational Model

The initial collector runs as a configured-target Go runtime:

- configuration declares registry instances and repository allowlists
- the collector polls those configured repository targets on a bounded interval
- scans emit durable facts into Postgres; the projector then promotes
  digest-addressed image truth into the graph
- the runtime mounts `/healthz`, `/readyz`, `/admin/status`, and `/metrics`

The workflow contract registers `oci_registry` as a fact-only collector family
for now. Claim-driven scheduling stays disabled until repository scan
partitioning, lease ownership, and retry semantics have live proof.

Required status fields:

- configured registries
- active repository scopes
- last completed generation per repository
- tag mutation counts
- manifest and index counts
- referrers supported or unsupported
- rate-limit and auth-failure counts
- unknown media type counts

Required metrics:

| Metric | Type | Labels | Purpose |
| --- | --- | --- | --- |
| `eshu_dp_oci_registry_api_calls_total` | Counter | `provider`, `operation`, `result` | Registry API call volume and failures for ping, tag listing, manifest fetches, and Referrers API calls. |
| `eshu_dp_oci_registry_tags_observed_total` | Counter | `provider`, `result` | Tag observations accepted into a bounded repository scan. |
| `eshu_dp_oci_registry_manifests_observed_total` | Counter | `provider`, `media_family` | Manifest, image-index, and descriptor observations by broad media family. |
| `eshu_dp_oci_registry_referrers_observed_total` | Counter | `provider`, `artifact_family` | Referrer artifact observations by bounded artifact family. |
| `eshu_dp_oci_registry_scan_duration_seconds` | Float64 histogram | `provider`, `result` | Repository scan latency before durable commit. |

Required spans:

| Span | Purpose |
| --- | --- |
| `oci_registry.scan` | One configured repository scan. |
| `oci_registry.api_call` | One registry API call with bounded provider and operation attributes. |

Metric labels must stay low-cardinality and must not include image names,
repository names, tags, digests, or private paths.

## Edge Cases

The design must account for:

- tag mutation between scans
- tag deletion
- repositories with zero tags
- digest-pinned runtime references with no tag
- multi-platform image indexes
- nested indexes
- unknown media types
- unsupported Referrers API
- registries that return Docker-compatible media types
- missing `Docker-Content-Digest` headers from older registry behavior
- computed manifest digests from exact response bytes when that header is
  missing
- rate limits and retry-after responses
- auth denied versus repository not found
- duplicate descriptors across repositories
- repeated generation replay

## Alternatives Considered

### Provider-Specific First Model

Rejected. ECR, GHCR, Docker Hub, Harbor, GAR, ACR, and Artifactory adapters are
useful, but the fact model should remain OCI-first so reducers do not learn
provider-specific image semantics.

### Treat Tags As Canonical Image Identity

Rejected. Tags move. Digest identity is the only safe canonical image anchor.

### Parse SBOMs In The Registry Collector

Rejected. SBOM parsing is a separate consumer. The registry collector should
preserve referrer descriptors and let the SBOM collector own document parsing
and package/vulnerability semantics.

### Ignore Unknown Media Types

Rejected. Unknown media types may be future supply-chain artifacts. Preserve
the descriptor and emit a warning instead of dropping evidence.

## Rollout Plan

1. Add the `oci_registry` collector kind and workflow contract.
2. Define repository, tag observation, manifest, index, descriptor, referrer,
   and warning fact schemas.
3. Implement repository scan, tag listing, manifest resolution, digest
   verification, and fact emission.
4. Add provider adapters for one OCI-compatible baseline and ECR.
5. Add projector scaffolding for `ContainerImage`, `ContainerImageIndex`,
   `ContainerImageDescriptor`, and mutable tag observations.
6. Add deployment trace enrichment for Kubernetes image references using
   digest-first registry graph truth.
7. Add DSL joins to Git/CI, AWS ECR, Kubernetes live image refs, SBOMs, and
   vulnerability facts.
8. Add claim-driven workflow scheduling after live configured-target proof.

## Acceptance Criteria

- Fixture tests cover tag mutation, digest-pinned references, multi-platform
  image indexes, missing Referrers API, and unknown artifact media types.
- Repeated scans are idempotent.
- Reducer/projector can materialize `ContainerImage` by digest without direct
  collector graph writes.
- Query path can answer which runtime or workload references a digest.
- Registry auth failures, rate limits, unsupported features, and tag mutations
  are visible in status and telemetry.

## References

- OCI Distribution Spec:
  https://github.com/opencontainers/distribution-spec/blob/main/spec.md
- OCI descriptor spec:
  https://github.com/opencontainers/image-spec/blob/main/descriptor.md
- OCI image manifest spec:
  https://github.com/opencontainers/image-spec/blob/main/manifest.md
- OCI image index spec:
  https://github.com/opencontainers/image-spec/blob/main/image-index.md
- OCI annotations:
  https://github.com/opencontainers/image-spec/blob/main/annotations.md
- AWS ECR private registry authentication:
  https://docs.aws.amazon.com/AmazonECR/latest/userguide/registry_auth.html
- Docker Registry token authentication:
  https://docs.docker.com/reference/api/registry/auth/
- Docker Hub registry overview:
  https://docs.docker.com/docker-hub/
- GitHub Container Registry:
  https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry
- JFrog Docker repositories:
  https://docs.jfrog.com/artifactory/docs/docker-repositories
- JFrog Docker repository catalog API:
  https://docs.jfrog.com/artifactory/reference/listDockerRepositories
