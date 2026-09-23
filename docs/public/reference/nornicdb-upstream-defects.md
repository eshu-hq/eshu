# NornicDB Upstream Defects `#400`–`#409`

The ten confirmed upstream defects (`orneryd/NornicDB#400`–`#409`) Eshu pins
around on `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf4…` (see
[NornicDB Pitfalls](nornicdb-pitfalls.md#which-build-the-pinned-build-means-here)
for build identification), with the shape to avoid and the safe
shape. Companion to [NornicDB Pitfalls](nornicdb-pitfalls.md): none of the
ten matches an existing `## Pitfall:` section there or on the query,
write-shape, path-predicate, or order-limit companions, so this index is the
reference listing. Status and evidence come from the #6786 exposure table
(`docs/internal/evidence/6786-nornicdb-400-409-exposure.md`); X1–X11 are
Eshu-found shapes outside `#400`–`#409` and are tracked there.

| Defect | Shape to avoid | What goes wrong | Safe shape / Eshu status |
| --- | --- | --- | --- |
| [#400](https://github.com/orneryd/NornicDB/issues/400) | function call in `RETURN` after `MATCH … WITH … MATCH` | 0 rows, or one all-null row | Split the read: run the `WITH`-chained `MATCH`es first, apply the function-call projection in a second statement (the #6761 two-statement loader shape; exposure table: fixed by #6761). |
| [#401](https://github.com/orneryd/NornicDB/issues/401) | `WITH … WHERE` using `IN` / `STARTS WITH` / `ENDS WITH` / `CONTAINS` | filter not applied | Avoid the shape; no Eshu production statement uses it. |
| [#402](https://github.com/orneryd/NornicDB/issues/402) | `UNWIND … MATCH … WHERE [NOT] EXISTS {…}` | predicate not evaluated | Do not rely on the `NOT EXISTS` guard alone; run the anchored update-existing read first with the same rows (canonical File create-missing stays correct that way). Owner accept-vs-fix call undecided; exposure row stands at 'Affected, latent'. |
| [#403](https://github.com/orneryd/NornicDB/issues/403) | inline property map or identity inequality inside `EXISTS {…}` | ignored | Same as #402: same guard, same ordering, same undecided owner call. |
| [#404](https://github.com/orneryd/NornicDB/issues/404) | `n.id` on a node-only `MATCH` when `id` is absent | internal node id instead of null | No Eshu production read hits it. Labels that can lack `id`: File, Directory, Module, Environment, CodeownerTeam, Rationale, DocumentationSection, KustomizeOverlay, ShellCommand, Parameter — keep it that way. |
| [#405](https://github.com/orneryd/NornicDB/issues/405) | string `+` with an `UNWIND` variable | expression text returned or stored | Avoid the shape; no Eshu production statement uses it. |
| [#406](https://github.com/orneryd/NornicDB/issues/406) | subscript on an `UNWIND` variable | expression text or a list | Avoid the shape; no Eshu production statement uses it. |
| [#407](https://github.com/orneryd/NornicDB/issues/407) | list literal wrapping a variable | expression text stored | Avoid the shape; no Eshu production statement uses it. |
| [#408](https://github.com/orneryd/NornicDB/issues/408) | `UNWIND … AS v WITH v …` | rest of the statement ignored | `MATCH`-seeded `WITH` is safe with the schema applied: canonical File update-existing, the Language Directory branch, and the orphan sweep all match Neo4j. |
| [#409](https://github.com/orneryd/NornicDB/issues/409) | `MATCH … WHERE … STARTS WITH/ENDS WITH … CREATE` | nothing written | Avoid the shape; no Eshu production statement uses it. |

Upstream has since closed all ten as completed (2026-09-18, after the v1.3.3 pin);
re-prove each row against the new pin before simplifying a workaround
([#6787](https://github.com/eshu-hq/eshu/issues/6787)).
