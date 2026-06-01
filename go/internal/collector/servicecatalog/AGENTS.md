# AGENTS.md — internal/collector/servicecatalog guidance

## Read First

1. `README.md` — package purpose, exported surface, and the payload-key
   contract.
2. `backstage.go` / `backstage_model.go` — Backstage manifest normalization.
<<<<<<< HEAD
3. `opslevel.go` / `opslevel_model.go` — OpsLevel `opslevel.yml` normalization,
   including provider-host repository URL derivation.
4. `facts_builder.go` — provider-agnostic fact envelope construction.
5. `envelope.go` — fact identity, redaction, and URL safety.
6. `go/internal/reducer/service_catalog_correlation_index.go` — the reducer
   index whose payload keys this package MUST honor exactly.
7. `docs/internal/design/563-service-catalog-manifest-fact-emitter.md` — the
   design memo and phased PR plan.
8. `docs/public/reference/collector-reducer-readiness.md` — source-truth
=======
3. `cortex.go` / `cortex_model.go` — Cortex `cortex.yaml` normalization,
   including git-provider repository URL derivation.
4. `cortex_scorecard.go` — Cortex scorecard descriptor normalization into
   carried-only scorecard_definition and scorecard_result facts.
5. `facts_builder.go` — provider-agnostic fact envelope construction.
6. `envelope.go` — fact identity, redaction, and URL safety.
7. `go/internal/reducer/service_catalog_correlation_index.go` — the reducer
   index whose payload keys this package MUST honor exactly.
8. `docs/internal/design/563-service-catalog-manifest-fact-emitter.md` — the
   design memo and phased PR plan.
9. `docs/public/reference/collector-reducer-readiness.md` — source-truth
>>>>>>> b715b446 (feat(servicecatalog): Cortex manifest fact emitter (producer slice, PR-3))
   boundary for `service_catalog_correlation`.

## Invariants

- Keep this package fixture-backed until the hosted runtime slice is explicitly
  opened. No HTTP clients, credentials, filesystem discovery, or runtime status.
- Do not import the reducer or query packages in production code. The reducer is
  imported only in `*_test.go` for the round-trip contract test.
- Do not add new fact kinds or change the schema version. This package emits
  into the existing `service_catalog.*` contract
  (`facts.ServiceCatalogSchemaVersionV1`).
- Payload-key fidelity is the highest risk. Any payload key change must be
  checked against `serviceCatalogEntityFromFact`,
  `serviceCatalogOwnershipFromFact`, and `serviceCatalogRepositoryLinkFromFact`
  in the reducer index. The round-trip contract test must still reach the
  intended outcomes.
- Non-over-admission: never emit `repository_id`, `service_id`, or `workload_id`
  from catalog text. A catalog name or owner cannot mint canonical truth. This
  includes scorecard facts: a scorecard score never mints canonical identity.
- Emit `repository_url` verbatim from the declared manifest URL. Do not
  pre-canonicalize into `normalized_url`; the reducer re-canonicalizes the value
  it reads, and a bare host/path key fails re-canonicalization and breaks
  exact/derived matching.
<<<<<<< HEAD
- OpsLevel-only: a repository is declared as `provider` + a `name` slug, not a
  URL. Expand only the known public hosts in `opslevelProviderHosts`
  (`github`, `gitlab`, `bitbucket`, `azure_devops`) into a `repository_url`.
  Never guess a host for an unknown or self-hosted provider; emit the slug as a
  name-only `repository_name` and let the reducer reject it. Guessing a host
  would manufacture a wrong derivation and risk a false correlation.
=======
- Cortex-only: a repository is declared as a git `provider` plus a `name` slug.
  Expand only the known public hosts in `cortexProviderHosts`
  (`github`, `gitlab`, `bitbucket`) plus the Azure `project`+`repository` split.
  Never guess a host for an unknown or self-hosted provider; emit the slug as a
  name-only `repository_name` and let the reducer reject it. Resolve
  multi-provider blocks in sorted provider order so stable fact ids stay
  deterministic.
- Cortex scorecard_definition and scorecard_result facts are carried-only: the
  reducer index loads but does not classify them. Keep a test asserting they do
  not change any entity's correlation outcome.
>>>>>>> b715b446 (feat(servicecatalog): Cortex manifest fact emitter (producer slice, PR-3))
- Strip token-bearing or query-string URLs before emission. Redacted operational
  links emit a `service_catalog.warning`, never a dropped entity.
- Degraded documents (unsupported version, missing name, duplicate entity) emit
  warnings, never silent drops. Parse multi-document manifests per document.

## Common Changes

- Add a provider (OpsLevel, ...) by adding a provider model and a
  `<Provider>ManifestEnvelopes` entry point plus fixtures and tests. Reuse the
  shared builders in `facts_builder.go`. Backstage and Cortex are the shipped
  precedents.
- Add live API collection only in a future runtime package with credentials,
  request budgets, redaction proof, health/readiness, metrics, and status.
- If payload shape changes, re-check the reducer index and re-run the round-trip
  contract test before landing.
