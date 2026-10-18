# internal/workloadid

Builds the canonical graph identifiers for `Workload` and `WorkloadInstance`
nodes.

## Why this is its own package

Two reasons, and the second is the durable one.

**The identifier changed under #5385.** It was built from the workload name
alone, so two repositories with a same-named workload produced the same id and
collapsed onto one node — with the measured blast radius and the approved
migration in `docs/internal/design/5385-workload-identity-key.md`. Making the
identifier a distinct type with exactly one constructor means the **compiler**
enumerates construction sites, which a hand-maintained list did not: that list
was wrong three times.

Both constructors take the repository id and scope the key by it
(`workload:<repo_id>:<name>`, `workload-instance:<repo_id>:<name>:<environment>`).
That is what kept the re-key an edit inside this package rather than an edit at
every caller: every production caller already passed its repository, so the flip
changed no call site. A blank repository, name, or environment yields the empty
id (fail-closed); a colon inside the name or environment is likewise rejected,
because the readable composite has no parser. The repository segment itself
carries colons (`repository:r_<hex>`) and is exempt.

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
- **A colon in the name or environment yields the empty id.** The composite has
  no parser, so a colon-bearing segment would read ambiguously. The repository
  segment is exempt: canonical repo ids carry their own `repository:` prefix.

## Sites that still build the string by hand

Three read-side callers concatenate the public-handle prefix rather than taking
a `WorkloadID`. They build **handles** (`workload:<name>`), not node ids, and
the handle format did not move with the re-key — but they are listed here
because the constructor invariant above would otherwise read as covering them:

| Site | What it builds | Why it stays |
| --- | --- | --- |
| `internal/query/impact/change_surface_resolvers.go` (`canonicalWorkloadIDCandidate`) | `"workload:" + target` for a bare target | Handle-rung input to the change-surface ladder: the bare name is matched within the caller grant, never as a node id. A scoped id passes through untouched (it already carries the prefix). |
| `internal/query/entity/workload_context.go` (equality chain) | `selector == "workload:" + normalized` | Handle comparison for the identity-only fallback: the selector is user input (handle or bare name), matched against known workload names. Response `id` values come from the constructor. |
| `internal/query/repository/catalog.go` (`catalogWorkloadKey` fallback) | `TrimPrefix(workload.ID, "workload:")` when `Name` is empty | Merge-key fallback only, and the key is namespaced by `RepoID` first, so split siblings still split. Graph and identity rows normally carry `Name` and never reach the fallback. |

The remaining `workload:`-prefixed values that are neither node ids nor handles
are the `shared_followup` / `reducer_workload_identity` entity keys
(`workload:<repo_id>`, built by `followupEntityKey`). They are matched by
repository downstream, never id-matched against `Workload` nodes, so the re-key
leaves them as they are (compatibility doc Q7).

## What this package is not

It is unrelated to the reducer's `reducer_workload_identity` fact and its
`DomainWorkloadIdentity` intent. That is a **different** key, `workload:<repo_id>`,
built by `followupEntityKey` in `go/internal/collector/repo/git/followup_facts.go`
(#7316), and reconciling the two is settled by compatibility-doc Q7 (entity keys
stay as they are; the graph handle is derived at repo-aware seams) rather than
by this package.
