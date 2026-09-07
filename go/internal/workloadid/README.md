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
  enumeration this package exists for. The invariant binds the **typed value**,
  and today that means the reducer write path — the code that decides projected
  graph truth. It is not yet true of the string; see below. Enforcement lives
  in-tree: the representation is opaque (pinned by `TestIdentifierTypesAreOpaque`),
  and `TestWorkloadIDsRouteThroughConstructors` in `internal/reducer` fails the
  four hand-built shapes, so the claim survives the next edit.
- **A blank segment yields the empty id**, never a bare prefix or an id with an
  empty segment — either would `MERGE` unrelated candidates onto one shared node.
  The reducer emission sites drop empty-id rows instead of emitting them.

## Sites that still build the string by hand

Three read-side callers concatenate the prefix rather than taking a `WorkloadID`.
They are unconverted deliberately — converting them is the re-key's work, not
this step's — but they are listed here because the invariant above would
otherwise read as covering them:

| Site | What it builds | Why it matters at re-key |
| --- | --- | --- |
| `internal/query/impact_change_surface_resolvers.go:107` | `"workload:" + target` | The sharpest one. The result is matched against graph nodes, so a re-key confined to this package silently stops the change-surface resolver matching anything. |
| `internal/query/entity_workload_context.go:261` | `"id": "workload:" + workloadName` | Emits the id into an API response body. |
| `internal/query/catalog.go:213` | `"workload:" + strings.TrimPrefix(identity.Name, "workload:")` | Normalises a possibly-prefixed name back into an id. |

The re-key must convert these or prove each one reads a value that was already
built by a constructor. Until then, the compiler's guarantee stops at the
reducer boundary.

## What this package is not

It is unrelated to the reducer's `reducer_workload_identity` fact and its
`DomainWorkloadIdentity` intent. That is a **different** key, built as
`"workload:" + filepath.Base(repoPath)` in
`go/internal/collector/gitrepo/git_followup_facts.go`, and reconciling the two is an open
question on #5385 rather than something this package settles.
