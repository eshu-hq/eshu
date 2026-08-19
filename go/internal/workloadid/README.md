# internal/workloadid

Builds the canonical graph identifiers for `Workload` and `WorkloadInstance`
nodes.

## Why this is its own package

Two reasons, and the second is the durable one.

**The identifier is about to change.** It is currently built from the workload
name alone, so two repositories with a same-named workload produce the same id
and collapse onto one node — issue #5385, with the measured blast radius and the
approved migration in `docs/internal/design/5385-workload-identity-key.md`.
Making the identifier a distinct type with exactly one constructor means the
**compiler** enumerates construction sites, which a hand-maintained list did not:
that list was wrong three times.

Both constructors already accept the repository id and deliberately ignore it.
That is what turns the re-key into an edit inside this package rather than an
edit at every caller.

**Identity construction should not carry a dependency closure.** This mirrors
`internal/repositoryidentity`, which owns `repository:r_<hex>` for the same
reason. A leaf package means any layer can build an id without taking on the
reducer's 74-package closure, and it survives the reducer split tracked under
#6053 — the type would otherwise have to move twice.

## Invariants

- **Stay a leaf.** No in-module imports. If a caller needs something from here
  that would require an import, the shape is wrong.
- **One constructor per identifier.** Any second way to build one defeats the
  enumeration this package exists for.
- **A blank segment yields the empty id**, never a bare prefix or an id with an
  empty segment — either would `MERGE` unrelated candidates onto one shared node.

## What this package is not

It is unrelated to the reducer's `reducer_workload_identity` fact and its
`DomainWorkloadIdentity` intent. That is a **different** key, built as
`"workload:" + filepath.Base(repoPath)` in
`go/internal/collector/git_followup_facts.go`, and reconciling the two is an open
question on #5385 rather than something this package settles.
