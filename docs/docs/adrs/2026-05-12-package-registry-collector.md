# ADR: Package Registry Collector

**Date:** 2026-05-12
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-05-09-optional-component-boundary.md`
- `2026-05-10-component-package-manager-and-optional-collector-activation.md`
- `2026-05-10-oci-container-registry-collector.md`
- Issue: `#24`

---

## Context

Eshu already reads manifests, lockfiles, import graphs, Dockerfiles,
Kubernetes image refs, Terraform, Helm, and CI/CD workflows from Git. That
proves what source repositories declare. It does not prove which packages or
artifacts exist in private feeds, which versions are published, which versions
are yanked or deprecated, which digest was served by a mirror, or which package
version a build pushed.

Issue `#24` asks for a package registry collector. The first live validation
targets are JFrog Artifactory and Amazon ECR because those are the registries
we can test directly. They are not the same source-truth boundary:

- ECR is an OCI/container registry. It belongs to the OCI registry collector
  contract and gives digest, tag, manifest, index, and referrer truth.
- JFrog Artifactory can serve OCI images and package-manager feeds. It is the
  first provider that should exercise both the OCI registry collector and this
  package registry collector.

This ADR defines the package registry side. It does not replace the existing
OCI container registry ADR.

## Source Contracts

The collector must use source-native package contracts before provider APIs:

- npm: packuments, version manifests, dist-tags, tarball integrity,
  repository fields, and dependency fields.
- PyPI: Simple/Index API, JSON API where needed, distributions, hashes, yanked
  state, and Core Metadata.
- Go modules: GOPROXY `.info`, `.mod`, `.zip`, and `@latest` endpoints.
- Maven: repository path layout, GAV coordinates, metadata, POM dependency
  model, classifiers, and checksums.
- NuGet: V3 service index, registration metadata, flat-container package
  content, `.nuspec`, and catalog where available.
- Provider repositories such as Artifactory, GitHub Packages, GitLab Package
  Registry, Google Artifact Registry, Azure Artifacts, AWS CodeArtifact, and
  Sonatype Nexus are adapters around those package-native contracts unless a
  provider-only metadata field is explicitly useful evidence.

Registry metadata must not become canonical dependency or ownership truth by
itself. Package feeds report package state. Reducers decide whether source,
build, CI/CD, runtime, and registry facts agree strongly enough to materialize
ownership or consumption.

## Decision

Add a future collector family named `package_registry`.

The collector owns:

- configured registry and feed discovery
- bounded package/version observation
- package identity normalization by ecosystem and feed
- package-native metadata parsing
- artifact coordinate, checksum, and source-hint extraction
- provider-auth and rate-limit failure classification
- typed fact emission

The collector does not own:

- canonical graph writes
- repository ownership admission
- dependency truth across source and lockfile evidence
- vulnerability severity decisions
- SBOM parsing
- answer shaping

## Initial Provider Support

Phase 1 should support the registries we can validate live:

| Provider | Collector lane | First scope | Why |
| --- | --- | --- | --- |
| Amazon ECR private and public | `oci_registry` | OCI repositories, tags, manifests, indexes, referrers where available | We can test it, it is common in AWS shops, and AWS documents OCI and Docker Registry HTTP API V2 support. |
| JFrog Artifactory Docker/OCI repositories | `oci_registry` | OCI repositories, tags, manifests, indexes, Artifactory repository shape, referrers on supported versions | We can test it, it is common in enterprises, and Artifactory exposes local, remote, and virtual registry topology that matters for provenance. |
| JFrog Artifactory package repositories | `package_registry` | npm, Maven, NuGet, PyPI, Go, Generic where configured | Same live test surface, but package semantics must stay ecosystem-specific instead of being flattened into OCI image facts. |

Do not describe ECR as package-registry support. If AWS package feeds are
needed, that is AWS CodeArtifact, not ECR.

## Recommended Next Providers

Support should expand in this order:

1. **Public ecosystem registries for contract fixtures:** npmjs, PyPI, Maven
   Central, NuGet.org, and the public Go proxy. These give stable,
   credential-free fixture and replay coverage for ecosystem semantics.
2. **GitHub Packages and GHCR:** common for source-adjacent organizations, with
   package and container visibility tied to repositories and organizations.
3. **GitLab Package Registry and GitLab Container Registry:** important for
   self-managed GitLab shops; GitLab supports both package feeds and OCI
   container images, but package formats have uneven maturity.
4. **Google Artifact Registry:** cloud-managed Docker/OCI, Maven, npm, Python,
   Apt, and Yum feeds; good counterpart to ECR for GCP users.
5. **Azure Container Registry and Azure Artifacts:** ACR covers OCI artifacts;
   Azure Artifacts covers Maven, npm, NuGet, Python, and universal packages.
6. **Sonatype Nexus Repository:** common enterprise package manager and proxy;
   useful once Artifactory semantics are proven.
7. **Harbor, Docker Hub, Quay, and CNCF Distribution:** OCI-focused registries
   for image, Helm, SBOM, signature, and attestation evidence. These belong to
   the OCI collector lane unless a provider also exposes package-manager feeds.
8. **AWS CodeArtifact:** package-feed complement to ECR for AWS shops. Support
   after Artifactory proves the package feed model, because CodeArtifact is
   mostly package-native API semantics plus AWS auth and repository domains.

## Scope And Generation

Collector instance:

- one configured registry or feed access boundary
- provider adapter and credential reference
- allowlisted packages, namespaces, groups, repositories, or prefixes
- optional rate and concurrency limits

Suggested scope kinds:

- `package_registry`
- `package_namespace`
- `package_name`
- `package_version`
- `artifact_repository`

Example scope IDs:

```text
npm://registry.npmjs.org/react
pypi://pypi.org/project/requests
gomod://proxy.golang.org/golang.org/x/mod
maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core
nuget://api.nuget.org/v3/Newtonsoft.Json
artifactory://jfrog.example/artifactory/libs-release-local/npm/@scope/pkg
```

Generation must be one bounded registry/package observation, not a full public
registry crawl. Use source-native revision, serial, checksum, timestamp, ETag,
catalog timestamp, or item hash where available. A completed generation means
the configured package scope was observed to completion or explicitly marked
partial with warning facts.

## Fact Families

Initial fact kinds:

| Fact kind | Purpose |
| --- | --- |
| `package_registry.package` | Registry, ecosystem, normalized package identity, raw name, namespace, scope/group, repository/feed identity, and visibility when known. |
| `package_registry.package_version` | Version, published time, yanked/unlisted/deprecated/retracted flags, dist-tags/latest aliases, checksum/digest, and artifact URLs. |
| `package_registry.package_dependency` | Package version, dependency identity, range, dependency type, target framework, marker, optional/excluded/dev/peer/runtime semantics. |
| `package_registry.package_artifact` | Tarball, wheel, sdist, jar, nupkg, module zip, or generic artifact coordinates, size, hashes, classifier, and platform tags. |
| `package_registry.source_hint` | Repository URL, homepage, SCM/project URL, build provenance when present, confidence reason, and normalization result. |
| `package_registry.vulnerability_hint` | Registry-provided advisory fields only. OSV, CVE, and severity policy belong to vulnerability-intelligence consumers. |
| `package_registry.registry_event` | Publish, delete, unlist, deprecate, yank, relist, and metadata mutation events where the source exposes them. |
| `package_registry.repository_hosting` | Provider/feed topology such as Artifactory local, remote, or virtual repository and upstream identity. |
| `package_registry.warning` | Auth denied, rate limited, unsupported format, metadata parse failure, partial generation, digest mismatch, stale mirror, package unavailable, and unknown dependency shape. |

Every fact must carry:

- `collector_kind=package_registry`
- `collector_instance_id`
- `scope_id`
- `generation_id`
- `source_confidence=reported`
- `fence_token`
- `correlation_anchors`

## Identity And Correlation Rules

Identity is ecosystem-specific:

- npm identity is registry plus normalized package name, including scope.
- PyPI identity follows Python package-name normalization, not display name.
- Go module identity is module path plus version in the selected GOPROXY
  context.
- Maven identity is group ID, artifact ID, version, classifier, packaging, and
  repository context.
- NuGet identity is package ID plus normalized version and feed.
- Generic artifacts require provider, repository, path/key, checksum, and
  metadata because they do not have a package-native dependency model.

Reducer-owned correlations should handle:

- repo publishes package through manifest, release tag, CI publish evidence,
  build provenance, or package source hints
- repo consumes package through manifest, lockfile, import, or build evidence
  matched to registry identity
- package depends on package using ecosystem-specific dependency semantics
- service consumes library through deployable-unit and image correlation
- package version came from build through Artifactory build-info or provenance
- unresolved or ambiguous package owners remain evidence-only

Do not mark homepage, repository URL, or package name similarity as exact
ownership without corroborating source, build, or release evidence.

## Graph And Query Contract

Projector/reducer ownership:

- A package-registry projector may materialize `(:Package)` and
  `(:PackageVersion)` nodes only from stable ecosystem identity. The first
  registry-promotion slice implements this with `uid`-keyed package and version
  nodes plus a package-local `HAS_VERSION` edge.
- Package ownership, publication, and consumption edges require reducer
  correlation. Registry metadata alone remains provenance.
- Package dependency edges use ecosystem-specific semantics and must preserve
  scope/type/marker detail so query surfaces do not flatten dev, peer, runtime,
  optional, target-framework, and excluded dependencies into one vague edge.

Suggested query capabilities after implementation:

- Which repos publish packages consumed by this service?
- What downstream repos may be affected by changing this package?
- Show all package versions published from this repo and whether the source
  commit is indexed.
- Which internal Artifactory packages depend on `org.example:core-api`?
- Is this package deprecated, yanked, unlisted, or vulnerable according to
  registry metadata?
- Which registry facts are unresolved or ambiguous because source ownership is
  not corroborated?

## Operational Model

The collector should run as a claim-driven Go runtime:

- configuration declares registry instances and credential references
- workflow coordinator enqueues bounded package/feed scopes
- collector workers claim package observations, heartbeat, emit facts, and
  complete claims through the shared workflow store
- the runtime mounts `/healthz`, `/readyz`, `/admin/status`, and `/metrics`

Required status fields:

- configured registries and ecosystems
- active package scopes
- last completed generation per scope
- package/version/artifact/dependency counts
- partial generation counts
- rate-limit and auth-failure counts
- yanked/deprecated/unlisted counts where available
- source-hint normalization failures
- freshness lag by registry and ecosystem

Required metrics:

- `eshu_dp_package_registry_observe_duration_seconds`
- `eshu_dp_package_registry_requests_total{ecosystem,status_class}`
- `eshu_dp_package_registry_facts_emitted_total{ecosystem,fact_kind}`
- `eshu_dp_package_registry_rate_limited_total{ecosystem}`
- `eshu_dp_package_registry_generation_lag_seconds{ecosystem}`
- `eshu_dp_package_registry_parse_failures_total{ecosystem,document_type}`
- reducer counters for exact, derived, ambiguous, unresolved, and rejected
  package-source correlations

Metric labels must stay low-cardinality. Do not put package names, module
paths, URLs, registry hostnames, scopes, versions, artifact paths, or private
feed names in metric labels.

## Redaction And Security

Private package names and private feed URLs can be sensitive.

Rules:

- credentials never appear in logs, facts, traces, warnings, or metrics
- private hostnames and package names are not metric labels
- source URLs from package metadata are evidence, not safe public text by
  default
- provider adapter logs use bounded failure class, ecosystem, provider, status
  class, and credential-ref hash
- package metadata that contains arbitrary README/body content is not stored in
  the first slice
- Artifactory build-info and generic metadata fields require an allowlist before
  raw keys or values are emitted

## Edge Cases

The design must account for:

- package names that normalize to the same identity
- same package/version served by different mirrors with different bytes
- mutable registry metadata over immutable artifacts
- yanked, deprecated, unlisted, retracted, and relisted versions
- dist-tags such as `latest` moving between versions
- prerelease and build metadata semantics
- missing checksums
- stale source URLs
- package transfer or namespace ownership changes
- private feed auth denied versus package not found
- rate limits and retry-after responses
- partial repository or catalog pagination
- dependency groups that do not mean runtime consumption
- platform-specific dependencies
- duplicate generation replay

## Cypher And Graph-Write Notes

Any graph write must use stable identity keys:

- `Package.id = <ecosystem>://<registry-or-feed>/<normalized-name>`
- `PackageVersion.id = <package-id>@<normalized-version>`
- artifact nodes use checksum/digest when present and provider path only as
  supporting provenance

Cypher writes must start from indexed labels and stable IDs. Relationship
`MERGE` identity must be narrow and idempotent; mutable fields such as
published time, deprecation message, source URL, and latest alias state belong
in `SET`, not in `MERGE` maps.

Before implementing graph writes, add schema/index support for package and
version identity, prove duplicate input rows do not create duplicate edges, and
run both Neo4j and NornicDB conformance for the statement shape.

## Implementation Slices

1. **Contract fixtures first:** add package identity normalization and fact
   payload tests for npm, PyPI, Go modules, Maven, NuGet, and Generic artifacts
   without live registry credentials. Implemented parser fixtures cover npm
   packuments, PyPI JSON, offline GOPROXY bundles, Maven POM XML, NuGet nuspec
   XML, and provider-specific Generic/JFrog metadata.
2. **Runtime extension seam:** add bounded runtime target config and an explicit
   ecosystem parser registry so npm, PyPI, Generic, Go modules, Maven, NuGet,
   and future ecosystems register source-native behavior without one opaque
   adapter switch. Implemented follow-up adds `collector-package-registry`,
   workflow coordinator planning for `package_registry` work items,
   claim-fenced metadata fetch from explicit `metadata_url` targets, runtime
   credential env resolution, and the `eshu_dp_package_registry_*` telemetry
   family.
3. **OCI live lane:** use the existing OCI registry ADR to implement ECR and
   JFrog Docker/OCI observation for repositories, tags, manifests, indexes,
   referrers, warnings, and redaction.
4. **JFrog package lane:** implement Artifactory package-feed observation using
   package-native contracts first, with Artifactory repository topology emitted
   only as provider hosting evidence.
5. **Reducer correlation lane:** add tests for exact, derived, ambiguous,
   unresolved, stale, and rejected package-source correlations before any
   canonical package ownership or consumption edges are promoted. The first
   graph-promotion sub-slice materializes `Package`/`PackageVersion` identity
   and keeps source hints provenance-only; ownership and consumption remain for
   reducer admission.
6. **Query lane:** expose package publication and consumption evidence only
   after graph truth and query truth agree for repo, service, and package
   surfaces. The first query sub-slice exposes bounded package/package-version
   identity reads from the canonical graph and explicitly omits repository
   ownership until reducer admission lands.
7. **Provider expansion lane:** add fixture-backed adapters for public ecosystem
   registries, then live-gated adapters for GitHub, GitLab, Google, Azure,
   Nexus, and CodeArtifact.

## Acceptance Criteria

- Collector emits versioned fact envelopes only; no direct graph writes.
- Facts are idempotent under at-least-once delivery.
- Phase 1 has live validation against ECR for OCI and JFrog for OCI/package
  feeds, with credentials redacted from all proof artifacts.
- Public ecosystem fixtures cover package, version, artifact, dependency,
  source hint, and deprecation/yank/unlisted/retraction state where supported.
- Package identity normalization is ecosystem-specific and feed-aware.
- Full-registry crawls are explicitly out of scope.
- Reducer tests cover exact, derived, ambiguous, unresolved, stale, and rejected
  package-source correlations before canonical ownership edges ship.
- Private registry auth and token redaction are proven before private feed
  collection is enabled.

## Research Checked

- AWS ECR documents OCI and Docker Registry HTTP API V2 support:
  <https://aws.amazon.com/documentation-overview/ecr/>
- AWS ECR pull-through cache covers Docker Hub, ACR, GHCR, GitLab Container
  Registry, and Chainguard, with OCI referrers surfaced through ECR APIs:
  <https://docs.aws.amazon.com/AmazonECR/latest/userguide/pull-through-cache.html>
- JFrog Artifactory supports Docker repositories, OCI repositories, local,
  remote, and virtual repository topology, and OCI Referrers API support on
  supported versions:
  <https://docs.jfrog.com/artifactory/docs/docker-repositories>
  <https://docs.jfrog.com/artifactory/docs/oci-repositories>
- GitHub Packages covers common package-manager registries and GHCR stores
  Docker/OCI images:
  <https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages>
  <https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry>
- GitLab supports package feeds for Maven, npm, NuGet, PyPI, Generic, Go, Helm,
  Composer, Conan, Debian, and RubyGems, and its container registry supports
  Docker V2 and OCI image formats:
  <https://docs.gitlab.com/ee/user/packages/package_registry/supported_package_managers.html>
  <https://docs.gitlab.com/user/packages/container_registry/>
- Google Artifact Registry supports Docker/OCI, Maven, npm, Python, Apt, and
  Yum, and Docker repository names use `LOCATION-docker.pkg.dev/PROJECT/REPOSITORY`:
  <https://docs.cloud.google.com/artifact-registry/docs/supported-formats>
  <https://cloud.google.com/artifact-registry/docs/docker/names>
- Azure Container Registry covers OCI artifacts, while Azure Artifacts covers
  Maven, npm, NuGet, Python, and universal packages:
  <https://learn.microsoft.com/en-us/azure/container-registry/container-registry-concepts>
  <https://learn.microsoft.com/en-us/azure/devops/pipelines/artifacts/artifacts-overview>
- Docker Hub supports OCI artifacts in addition to container images:
  <https://docs.docker.com/docker-hub/repos/manage/hub-images/oci-artifacts/>
