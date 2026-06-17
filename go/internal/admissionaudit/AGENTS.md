# AGENTS.md - internal/admissionaudit guidance

## Read first

1. `go/internal/admissionaudit/README.md`
2. `tests/fixtures/product_truth/README.md`
3. `docs/internal/agent-guide.md` "Bootstrap And Correlation Truth"
4. `go/internal/reducer/README.md`
5. `go/internal/query/README.md`

## Invariants this package enforces

- `admissionaudit` is pure comparison. Do not add database, graph, API, MCP, or
  filesystem collection logic beyond `LoadSuite`.
- Fixture intent is independent product truth. Do not generate expected intent
  from Eshu output.
- Non-admitted decisions must stay provenance-only in the audit snapshot.
- Admitted decisions must agree across reducer decision, graph fact, API
  readback, and MCP readback.
- Duplicate decision IDs and stale admitted decisions are audit failures.

## Common changes

- Add a new audit field by updating `Suite` or `Observation`, then add a red
  test showing the failure that should be reported before changing `Audit`.
- Add a product-truth suite by updating
  `tests/fixtures/product_truth/manifest.json`, expected JSON, and the verifier
  script. Keep fixture text public-safe.

## Failure modes

- Missing decision failures usually mean the reducer did not persist admission
  decision rows for a case that already has fixture intent.
- Unexpected canonical writes mean a rejected or ambiguous case leaked into
  graph truth.
- Readback disagreements mean API and MCP no longer expose the same bounded
  admission-decision shape.
- Logical duplicate decisions mean the reducer emitted more than one decision
  row for the same case/domain/scope/generation identity, which hides an upsert
  or idempotency-key bug.
- Missing canonical writes mean an admitted decision did not record its own
  canonical write, so a stale or external graph fact could mask a reducer bug.
- Readback truth disagreements mean an API or MCP surface returned a state that
  disagrees with the reducer decision even if the two surfaces agree with each
  other.

## What not to change without design review

- Do not import `internal/reducer`, `internal/query`, `internal/mcp`,
  `internal/storage`, or graph packages here. The dependency direction must stay
  from collectors/tests/scripts into this package, not the reverse.
