# extraction

Advisory **collector extraction readiness** for component diagnostics. It tells
contributors and operators whether a collector family should leave the core
repository, and when it should not, exactly which criteria are unmet.

This package is informational. It never moves code, disables a collector,
changes a manifest, or touches runtime behavior.

## What it produces

For each collector family it returns a `Readiness` with one of four
classifications:

| Classification | Meaning |
| --- | --- |
| `keep_in_tree` | Correlation-critical core collector. Stays in tree until a separate architecture gate proves a split keeps correlation correct. |
| `extraction_candidate` | Eligible family with no unmet criteria, not yet promoted to run out of tree. |
| `blocked` | Eligible family with at least one unmet criterion, reported as concrete blockers. |
| `external_ready` | Out-of-tree proof complete and the family runs out of tree as its default path. |

The per-criterion checklist mirrors the "Extraction Criteria" table in
[collector-extraction-policy.md](../../../docs/public/reference/collector-extraction-policy.md):
`source_coupling`, `fact_contract`, `scope_generation`, `trust_boundary`,
`runtime_behavior`, `release_cadence`, and `proof_surface`.

## Design

- `extraction.go` defines the `Classification`, `Criterion`, `State`,
  `CriterionResult`, `Profile`, and `Readiness` types.
- `evaluate.go` holds `Evaluate`, the deterministic classifier. It is total: a
  `Profile` that omits a criterion fails closed (the missing criterion is treated
  as `unmet`), so an authoring gap never silently passes. `SortReadiness` gives a
  stable presentation order.
- `catalog.go` holds the evidence-based `Profile` set and the `Catalog` and
  `Lookup` accessors. Membership tracks the policy doc's "Keep In Tree" and
  "Extraction Candidates" lists.

## Honesty contract

The catalog reflects real repository state, not aspiration:

- Only PagerDuty has completed the out-of-tree boundary proof, so it is the one
  `extraction_candidate` with every criterion met. It is **not** `external_ready`
  because the in-tree collector stays the production correlation path.
- Other named vendor candidates (Jira, Confluence/documentation, Grafana, Loki,
  Tempo, Prometheus/Mimir, vulnerability intelligence) are `blocked`: their
  trust-boundary, hosted-runtime, and proof criteria are unmet because no
  component package, hosted worker, or extraction proof exists for them yet.
- No family is `external_ready` today. That classification is exercised only by
  the engine tests via a synthetic profile, so the catalog never over-claims.

When a family's real evidence changes, update its `Profile` in `catalog.go` and
the matching expectation in `catalog_test.go`.

## Surfaces

The CLI command `eshu component extraction-readiness [family]`
(`go/cmd/eshu/component_extraction_readiness.go`) renders the catalog or a single
family, with `--json` and `--verbose`.

## Tests

- `evaluate_test.go` covers all four classifications including blocked-by-schema
  and blocked-by-runtime, the fail-closed and dedupe paths, and sort order.
- `catalog_test.go` validates profile completeness, the expected classifications,
  and the honesty contract above.

```bash
cd go && go test ./internal/extraction ./cmd/eshu -count=1
```
