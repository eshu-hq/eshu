# supply

Path namespace for the query package's `chain` family (issue #6642,
`docs/internal/naming.md` rule 3: a glued compound name splits into nested
directories rather than staying glued). This directory owns no runtime
behavior and declares no Go package of its own.

| Child | Package | Owns |
| --- | --- | --- |
| `chain/` | `chain` (commonly imported as `supplychain`) | The supply-chain query hub: the `Handler` HTTP surface and the read-model ports it serves. See `chain/README.md` for the full boundary and `chain/AGENTS.md` for per-symbol export list. Children: `advisory/` (the advisory catalog and evidence read models), `impact/` (the impact findings, explain, aggregate, readiness, and runtime-context read models), `alerts/` (the security-alert reconciliation Postgres reads). |

`supplychain` was the pre-#6642 glued name; `supply/chain` is its destuttered
form. Root's `compat_supply_chain.go` keeps every pre-move exported spelling
for callers that reach these types through package `query`.
