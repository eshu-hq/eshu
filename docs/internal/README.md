# Internal Docs

This directory holds maintainer-only documentation for Eshu.

- Files in `docs/internal/` are not part of the public MkDocs site.
- Public docs live under `docs/public/` and every public `.md` file must be
  linked from `docs/mkdocs.yml` (enforced by `scripts/verify-docs-catalog.sh`).
- Machine-readable validation catalogs live under `specs/` (for example,
  `specs/remote-validation-proofs.v1.yaml`), not as per-slug Markdown stubs.
- Source-tree package orientation lives in package-local docs under `go/`; do
  not re-document that layout here.
- Before adding or editing any page here, load the **`publish`** skill and
  follow [Writing For Agents](writing-for-agents.md) so internal guides stay
  concise, legible, and free of session shorthand.
- Never commit ephemeral PR proof notes, raw `.sql` packets, arbiter fork
  rulings, PR review limits, agent implementation plans, extraction-prep
  drafts, or dated point-in-time reviews here or under `go/`
  (`scripts/verify-no-ephemeral-docs.sh`). Do not append `## Evidence`,
  `## Evidence Notes`, `## Move evidence`, or `## ... (#NNNN)` PR-changelog
  sections to `go/**/*.md` or `docs/public/**/*.md`. Put PR proof in GitHub PR
  descriptions or CI artifacts, and arbiter rulings or extraction checklists in
  GitHub issue comments.
- Internal specs and plans must be updated or removed when the active codebase
  changes. Treat them as current guidance only when they still match the repo.
- The [Correlation Atlas](correlation-atlas/README.md) maps which collector
  feeds which reducer correlation, what starts a run, and what starts it again
  when another source changes.

## Living Design & Decision Core (`docs/internal/design/`)

Every file in `docs/internal/design/` must belong to one of three categories:

1. **Lockstep test-read specs** (`specs/ci-docs-lockstep-paths.txt`): asserted
   directly by Go unit tests.
2. **Living cross-cutting ADRs & contracts** (cataloged below): durable
   architectural decisions or platform-wide invariants referenced across
   multiple packages or repos.
3. **Active open-issue designs**: short-lived designs tied to an OPEN GitHub
   issue, retired once the behavior ships and moves into code and public docs.

### Lockstep Test-Read Specs

| File | Why it stays |
| --- | --- |
| [430-nornicdb-graph-search-split.md](design/430-nornicdb-graph-search-split.md) | Curated search-document split contract (asserted by lockstep tests). |
| [5351-ifa-materialized-edge-known-gaps.md](design/5351-ifa-materialized-edge-known-gaps.md) | Known gaps register for IFA materialized-edge coverage (asserted by lockstep tests). |

### Living Cross-Cutting ADRs & Contracts

| Area | File | Contract |
| --- | --- | --- |
| Graph backend | [7331-neo4j-primary-graph-backend-decision.md](design/7331-neo4j-primary-graph-backend-decision.md) | Neo4j as canonical primary graph backend; NornicDB secondary policy. |
| Fact & SDK contracts | [contract-system-v1.md](design/contract-system-v1.md) | Typed fact-envelope and versioned SDK compatibility contract. |
| Fact IDL | [4574-fact-idl-decision.md](design/4574-fact-idl-decision.md) | Fact-schema IDL decision record. |
| Incident SQL | [4683-incident-routing-sql-decision.md](design/4683-incident-routing-sql-decision.md) | Incident routing SQL and fact-schema storage decision. |
| Reducer execution | [10861-reducer-execution-model.md](design/10861-reducer-execution-model.md) | Core reducer execution, leasing, and concurrency model. |
| Derived-fact governance | [4784-reducer-derived-fact-governance.md](design/4784-reducer-derived-fact-governance.md) | Governance rules for reducer-derived internal facts. |
| Cross-scope ownership | [5007-cross-scope-node-ownership.md](design/5007-cross-scope-node-ownership.md) | Postgres owner-ledger arbitration for shared graph node UIDs. |
| Graph projection policy | [5472-graph-projection-policy.md](design/5472-graph-projection-policy.md) | Exact-only canonical graph projection vs Postgres-only evidence. |
| Retract registry | [6228-materialized-edge-retract-registry-contract.md](design/6228-materialized-edge-retract-registry-contract.md) | Materialized-edge retract registry ownership and parity rules. |
| Code provenance | [2222-resolution-provenance-code-edges.md](design/2222-resolution-provenance-code-edges.md) | Code-edge resolution provenance and confidence semantics. |
| Workload identity | [5385-workload-identity-key.md](design/5385-workload-identity-key.md) | Canonical workload identity key design. |
| Workload cutover | [5385-workload-identity-migration.md](design/5385-workload-identity-migration.md) | Workload identity key migration and compatibility rules. |
| Config artifacts | [7126-ingest-time-config-artifacts.md](design/7126-ingest-time-config-artifacts.md) | Ingest-time configuration artifact read model (protocol companion in #7126). |
| IFA conformance | [4389-ifa-conformance-platform.md](design/4389-ifa-conformance-platform.md) | Determinism, replay, and fault-injection conformance layers. |
| API & OpenAPI | [3738-openapi-discipline.md](design/3738-openapi-discipline.md) | OpenAPI parity, error-shape, and route-discipline rules. |
| Route registry | [5584-route-serves-data-registry.md](design/5584-route-serves-data-registry.md) | Route serves-data registry contract for query and MCP surfaces. |
| MCP router | [8759-mcp-core-router-tool-set.md](design/8759-mcp-core-router-tool-set.md) | Core MCP router tool set and prompt-budget discipline. |
| Repo ecosystem | [eshu-ecosystem-repository-migration.md](design/eshu-ecosystem-repository-migration.md) | Satellite repository extraction and deployment boundary contract. |
| Naming conventions | [naming-remediation.md](design/naming-remediation.md) | Cross-package naming and stutter-prevention rules. |
| Logging & signals | [observability-standards.md](design/observability-standards.md) | Cross-service structured logging and signal standards. |
