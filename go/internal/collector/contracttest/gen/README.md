# contracttest-gen

## Purpose
Reads `specs/collector_fact_contract.v1.yaml` (the source-of-truth fact-shape
contract spec) and emits `go/internal/collector/contracttest/contract_data.go`
with the corresponding Go contract variables. Pass `-check` to verify the
committed output is current without writing.

## Usage
```bash
# Write mode (regenerate from spec):
scripts/generate-contracttest.sh

# Check mode (verify no drift):
scripts/verify-contracttest.sh
```

## Ownership boundary
This command owns the YAML-to-Go contract code generation. It does not own the
contract types (`Contract`, `FactKindShape`) — those are defined in the parent
`contracttest` package. It does not own the YAML spec — that is owned by Epic A
(#3736) and governed by the collector contract spec process.

## Invariants
- Generated output is committed to the repo.
- `-check` mode must exit non-zero when the committed output does not match
  what the generator would produce from the current spec.
- The generator must produce deterministic output (same spec → same bytes).
- No runtime imports (database, HTTP, queue, graph).

## Dependencies
- `gopkg.in/yaml.v3` — YAML spec parsing.
- Parent `contracttest` package — contract types are consumed by generated code.

## Related docs
- `specs/collector_fact_contract.v1.yaml` — source-of-truth spec.
- `go/internal/collector/contracttest/README.md` — parent package docs.
