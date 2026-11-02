# Unconditional Payload Markers Follow Content, Not Key Names (#9781)

The anchor-scoped relationship load kept `argocd_applications` and
`argocd_applicationsets` as `LIKE ANY` terms. Every parsed file carries both keys,
usually null, so those terms matched every `file` fact: 8,249 of 8,249 in a 5%
sample from the QA environment. A second 5% sample held 7,634 null values and only
2 non-empty ones. `file` facts are about 81% of stored bytes, so the narrowed load
still held most of the fleet.

## The Change

- A bucket marker now matches what `jsonb` prints for a bucket that holds a row:
  `"argocd_applications": [{` and `"argocd_applicationsets": [{`. A null, `[]`,
  `{}` or string value does not match, and neither does the key name inside a
  string, because its quotes are escaped there.
- `"flux_git_repositories": [{` is a new marker. `NormalizeRemoteURL` decodes
  percent-encoding, so `my%2Dapp` resolves to a remote ending in `my-app` while the
  row holds neither that token nor any alias. No catalog-derived anchor can reach
  such a row. Flux evidence comes only from this bucket, so selecting on it covers
  every spelling.
- The key-name markers had been hiding that gap, and a second one. The deferred
  terms drop `RemoteURL`, so a Flux row whose remote differs from the repository's
  name was selected only by the key name. With the ArgoCD markers narrowed alone,
  `TestDeferredBackfillResolvesFluxManifestWithoutAnAnchor` failed with
  `loaded_facts=0`. The Flux marker closes both.
- `"artifact_type":"argocd"` and `kind: applicationset` are gone. `jsonb` never
  prints the compact spelling, and `kind: application` already matches the other.
- The list moved to `relationships.UnconditionalPayloadMarkers`, next to the
  readers it mirrors, and `CatalogNonRepoIDPayloadAnchors` moved with it. The
  superset tests held copies of both, so they could not fail on a real change.
  They now read the production values, and their harnesses render `jsonb` spacing.

The SQL statements are unchanged. Only the `$1` term list differs.

## Accuracy Proof

`anchor_parity_test.go` and `TestUnconditionalPayloadMarkers*` compare discovery
over the narrowed load with discovery over every corpus row. The corpus now covers
a file row for each structured route (Terraform, Terragrunt, Helm chart, Helm
values, Kustomize, Dockerfile, Jenkins, Flux, ArgoCD Application), the raw `kind:`
route, percent-encoded Flux URLs, and generator resolution starting from a file
row. All carry the empty ArgoCD keys, so a route the alias anchors miss now fails.
`TestUnconditionalPayloadMarkersFollowParserOutput` runs the real parser, so the
`[{` spelling is held to what it writes.

| Load, corpus | Main rows | Main edges | #9209 alone rows | Rows after | Edges after | Noise file | Negative |
|---|---|---|---|---|---|---|---|
| Anchor-scoped, in memory, 75 rows | not run | not run | 55 | 25 | 19 of 19 | 20 to 0 | 10 to 0 |
| Anchor-scoped, PostgreSQL 18.6, 77 rows | 77 | 19 of 19 | 57 | 26 | 19 of 19 | 20 to 0 | 11 to 0 |
| Deferred, PostgreSQL 18.6, 77 rows | 20 | 17 of 22 | 21 | 21 | 19 of 22 | 0 | 1 to 0 |

The owner-token fix in
[9209-catalog-anchor-namespace-token.md](9209-catalog-anchor-namespace-token.md#slug-aliases)
added a slug reference and two rows that must stay out, and the baselines above were
not rerun on that corpus. On it, the anchor-scoped load takes 26 of 78 rows in memory
and 27 of 80 on PostgreSQL 18.6, with 20 of 20 edges. The deferred load takes 22 of
80 rows and 20 of 23 edges, the same 3 short of the fleet. No noise or negative row
loads in any of them.

Main is merge base `aaf386a87a` with the same corpus and harness, and the noise and
negative columns start from #9209 alone. Closure and per-commit share the
anchor-scoped load. The main edge count there is the load's, because per-commit on
main also drops the control-config edge, as the #9209 note records.

Against main the deferred load loses no edge and gains two: the control-config
`ARGOCD_APPLICATIONSET_TEMPLATE_SOURCE` edge from #9209 and a
`FLUX_GIT_REPOSITORY_SOURCE` edge. The Flux gain is the fixture's: that row has no
ArgoCD keys, so the key-name terms missed it, and a real parsed file has the keys.
The percent-encoded Flux rows do have the keys, so main selects them too; the marker
is what keeps them now that the key names are gone. The deferred test's reference
is the same statement with the key-name terms added back, and the load lost nothing
against it. The 3 edges it lacks against the fleet are the same 3 main lacks, the
known limit below. Whitespace variants exist only in the live corpus, because
`jsonb` normalizes them on input.

No-Regression Evidence: `ListOnboardedRepoScopedRelationshipFactRecordsQuery` on
real schema and migrations, PostgreSQL 18.6, 100,000 synthetic `file` rows with 94
`parsed_file_data` keys (874 MB, mean payload about 12.5 kB text), 800 partitions,
13 key-name terms against 12 content terms, six rounds with alternating first mover.
The plan is the same: a nested loop over the latest generations into an index scan
on `fact_records`, then the `LIKE ANY` filter.

| Measure | Key-name terms | Content terms |
|---|---|---|
| Rows returned | 100,000 | 43 |
| Payload bytes returned | 1,253,497,355 | 393,281 |
| `EXPLAIN ANALYZE` median | 10,958 ms | 13,097 ms |
| Full fetch, two runs | 13.50 s, 13.33 s | 13.14 s, 13.28 s |

Server execution rises 19.5%. No row stops early on a key-name hit any more, so
each one tests every term, though that cause is inferred, not measured. The fetch
stays level because the 1.25 GB is no longer sent. At 330,000 rows that scales to
about 44 s of fetch, which is an inference from one corpus size, not a measurement.
The deferred statement on one partition of that corpus took 0.37 ms before and
0.39 ms after with the same plan; that partition admits no rows, so this shows only
that the term change costs nothing there.

No-Observability-Change: no metric, span, or log changes. Row count per pass stays
invisible to an operator, which #9781 tracks. After deploy, read `pg_stat_statements`
rows per call for the statement above.

## Behavior Changes

- The anchor-scoped load keeps every edge. The deferred load keeps every edge and
  gains the two named under Accuracy Proof. Selection changes for both.
- `DeferredCatalogFingerprint` hashes `$1`, so every partition memo misses once
  after deploy and that pass reloads in full.
- Flux GitRepository rows now load on every pass, like ArgoCD rows, whatever the
  catalog holds. The QA environment holds 1 such file row, and non-empty ArgoCD and
  Flux buckets total 224 of 163,723 file rows.

## Limits

The family predicate on the deferred statement admits a `file` row only by path or
by the Flux bucket, so a non-empty ArgoCD bucket at an ordinary path never reaches
the anchors. That is why the deferred load misses 3 fleet edges above: they come
from `file`-only rows at `apps/` paths, with no paired `content` row. The limit
predates this change and is pinned to migration 059. A `content` fact at an `apps/`
path is admitted and yields its edge (`route-raw-kind`), so this fixture overstates
the loss for a manifest that has one.

The tolerant decoders skip a malformed element, so `["junk", {...}]` yields a row
the `[{` spelling misses. The parser writes only arrays of objects, which
`TestUnconditionalPayloadMarkersFollowParserOutput` holds for the three buckets.

With no non-repo_id alias in the whole catalog the markers are suppressed, as they
were before. A catalog with no repository names is not a real shape.

The `RemoteURL` anchor from #9209 is now redundant for evidence, because the Flux
marker selects those rows. It stays, and so does its known limit: a remote ending
in `/repository` adds the fleet-wide term for that repository's pass.
