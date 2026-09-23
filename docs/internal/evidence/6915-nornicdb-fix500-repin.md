# NornicDB re-pin to the fix-500 self-built image (#6915)

Change: the default NornicDB backend pin moves from
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-499-6ac958a9@sha256:fc90a2c3...` to
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`
(amd64 child `c4a2116e...`, arm64 child `523137af...`). The same files move
together as in the fix-499 re-pin ([6162-nornicdb-fix499-repin.md](6162-nornicdb-fix499-repin.md)).
Prior evidence notes stay untouched as historical records.

What the image is: upstream orneryd/NornicDB main at `6ac958a9` (the exact
commit the fix-499 image was built from) plus the ORDER BY key fix from
[orneryd/NornicDB#502](https://github.com/orneryd/NornicDB/pull/502), branch
`fix/500-orderby-preprojection` at `e022384c`. #502 is not merged upstream: the
NornicDB owner is landing the parser refactor (orneryd/NornicDB#492) before
other changes, so Eshu carries the fix in its own image until a later upstream
main contains it. Built with
`docker buildx build --platform linux/amd64,linux/arm64 -f docker/Dockerfile.amd64-cpu`
and pushed to the public eshu-hq GHCR package.

Why: #502 fixes orneryd/NornicDB#500. After a relationship pattern or
`OPTIONAL MATCH`, NornicDB dropped ORDER BY keys that were not returned
columns, and it split function keys such as `coalesce(t.id, t.uid)` on
whitespace. The two allowlisted #6915 statements (the Function top-N read and
the DEPLOYS_FROM read) therefore returned rows in storage order, and with
`LIMIT` they returned the wrong rows. This change retires both allowlist
entries in the same commit as the image bump, because the differential gate's
stale-entry guard fails on any entry that no longer matches a divergence.

Proof, run against the pushed image by digest next to
`neo4j:2026-community@sha256:eabfbb04...`:

- `docs/internal/evidence/6915-orderby-shim.py`: 21 of 23 statements return
  the same rows. On the fix-490 pin 12 differed on every run. Both allowlisted
  shapes (`F1_exact_limit`, `D1_exact_limit`) now match. The two remaining
  differences are a single-key `ORDER BY e.name` whose two tied rows come back
  in either order (unspecified in Cypher) and `WITH ... ORDER BY ... RETURN`,
  which #502 does not change.
- `docs/internal/evidence/6915-orderby-minimal.py`: 9 of 10 match; the tenth
  is the same single-key tie.

The remaining ORDER BY allowlist entries do not depend on sort-key
resolution: the code-story and CALLS entries are `missing`-tier dialect
splits, the `file_count` entry excuses rows tied on the sort key, and the
path entry excuses run-scoped generation ids. The Go re-sort in
`listMostComplexFunctions` (#6938) stays as defence in depth.

No-Observability-Change: no metric, span, log field, or status contract
changes. The image swap is observable through the existing backend image
assertions and the differential gate.

No-Regression Evidence: #502 was benchmarked upstream with before/after
interleaved runs (`benchstat`, `-count=6`) on the untouched traversal and
index-order paths, with no change in time, bytes, or allocations; the fixed
paths pay one extra projected value per row per hidden sort key.
