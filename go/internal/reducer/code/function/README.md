# code/function

Documentation namespace for the reducer's function-level code analysis
families (issue #6061, step 3 of `docs/internal/design/reducer-target-tree.md`).
This directory owns no runtime behavior. Its only child is its own Go
package:

| Child | Package | Owns |
| --- | --- | --- |
| `summary/` | `summary` | Durable value-flow function-summary persistence: effects, param-level taint sources, and the FunctionID->graph-uid map |

## What stays in the reducer root

- `registry_additive_domains.go` and `defaults_additive_domains_incident_code.go`:
  the composition root wires `summary.Definition()` and
  `summary.Handler` into `DefaultHandlers`/`Registry`, so this
  wiring cannot sit below root.
