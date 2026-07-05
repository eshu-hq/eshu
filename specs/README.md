# Specs

`specs/` holds machine-readable contracts that are consumed by tests and
documentation.

- `capability-matrix.v1.yaml` plus `capability-matrix/*.yaml` define
  user-facing capability behavior by runtime profile.
- `capability-catalog.v1.yaml` is the editorial overlay for the capability
  catalog: display names, owner packages, maturity overrides, known gaps, linked
  issues, docs, and the exemption/non-MCP-surface lists. It is reconciled with
  the matrix and the live MCP registry by `go/cmd/capability-inventory` into
  `go/internal/capabilitycatalog/data/catalog.generated.json`. See
  `docs/public/reference/capability-catalog.md`.
- `evidence-continuity.v1.yaml` maps the roster of GA and gated
  evidence-centric public capabilities to source fact, projection/read-model,
  API, MCP, empty-state, and negative evidence-loss proof. It is enforced by
  `scripts/verify-evidence-continuity.sh`.
- `language-feature-parity-ledger.v1.yaml` maps public language/config parser
  feature claims to implementation files, test files, docs, parser-backing
  class, deterministic no-provider posture, read surfaces, and gap-tracking
  issues. It is enforced by `scripts/verify-parser-relationship-kit.sh`.
- `fact-kind-registry.v1.yaml` maps every first-party fact kind to its schema
  version, lifecycle owner, reducer domain, projection hook, admission hook,
  read surface, truth profile, and no-provider posture. It generates
  `go/internal/facts/fact_kind_registry.generated.go` and
  `go/internal/facts/FACT_KIND_REGISTRIES.md`, and is enforced by
  `scripts/verify-fact-kind-registry.sh`.
- `replay-depth-requirements.v1.yaml` declares the replay depth-requirement
  taxonomy (C-13, #4366): the retractable graph node types (kept byte-equal to
  `cypher.RetractableNodeEntityLabels()` by a lockstep test), the static
  retractable graph edge types (kept byte-equal to
  `cypher.RetractableEdgeTypes()`), and the reducer drain. The replay-coverage
  gate uses it — plus the fact-kind registry projections and the implemented
  collectors — to require a depth scenario_type per applicable surface
  (delta/fault/ordering/crash/cost) and list the missing pairs. Consumed by
  `go/internal/replaycoverage` and
  `scripts/verify-replay-coverage-gate.sh`.
- `authorization-catalog.v1.yaml` defines the v1 built-in roles, explicit
  action grants, data classes, permission families, bootstrap-owner posture, and
  custom-policy deferral that enrich every generated capability catalog entry.
  See `docs/public/reference/authorization-catalog.md`.
- `product-claims.v1.yaml` is the public claim-to-proof ledger for broad README
  and docs prose that a single capability marker cannot prove. It binds source
  `product-claim` markers and whole-line quotes to capabilities, owner paths,
  generated surfaces, deterministic proof, catalog proof signals, semantic
  posture, generated surface counts, and issue state, and is checked by
  `go/cmd/capability-inventory -mode docs`. Live issue-state verification runs
  in `.github/workflows/product-claim-ledger.yml`.
- `backend-conformance.v1.yaml` defines graph-backend capability classes for
  official adapters.
- `scale-lab-corpus.v1.yaml` defines the representative scale-lab corpus,
  privacy, metric, and threshold contract that gates reducer, graph-write,
  API/MCP, and correlation fanout scale work. See
  `docs/public/reference/local-testing/representative-corpus-suite.md`.
- `scale-benchmark-artifact.v1.yaml` defines the public-safe benchmark result
  artifact required for large-corpus ingestion, reducer drain, graph-write,
  API, MCP, retry/dead-letter, memory, backend, and before/after proof. See
  `docs/public/reference/local-testing/scale-benchmark-artifact.md`.
- `capability-budget-proof.v1.yaml` defines the public-safe per-capability
  proof artifact that binds capability-matrix `p95_latency_ms` and
  `max_scope_size` rows to measured API/MCP budget evidence. It is enforced by
  `scripts/verify-capability-budget-proof.sh`.

Treat edits here as contract changes. Update matching docs and verification in
the same PR.
