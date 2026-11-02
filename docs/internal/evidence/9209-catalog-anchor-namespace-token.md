# Catalog Anchors Skip The Repository Namespace Token (#9209)

`CatalogPayloadAnchors` picks the substring each `LIKE ANY` term of the
anchor-scoped relationship load tests. For a canonical repo id
(`repository:r_<8-hex>`) it picked `repository`: both tokens are 10 characters
and the old `>` comparison kept the first. Every Git `content` and `file`
payload stores its own `repo_id`, so that term matched every row and the
narrowed load became a fleet load.

## The Change

- The anchor of an alias with a slash is its last token other than `repository`.
  The anchor of any other alias is its longest token other than `repository`,
  later token on a tie. An alias with no other token keeps `repository`, because a
  repository with no anchor would lose evidence.
- The last token of `RemoteURL` is an anchor. Flux resolves its source URL by
  equality with `RemoteURL`, so no alias anchor reaches that row.
- `ResolveArgoCDGeneratorConfigRepos` returns the control repository when a
  generator reads it (#7778). The old skip was stale. On the base commit the
  control repository's files did load, but `BackfillScopedCatalog` left its
  catalog entry out, so the per-commit edge had nothing to resolve through.

The last two changes exist because the first exposed them. With token selection
alone, the load kept 5 of 7 edges: the fleet-wide term had been selecting those
rows by accident.

## Accuracy Proof

`go/internal/storage/postgres/backfill/targeted/anchor_parity_test.go` compares
discovery over the narrowed load with discovery over every corpus row, for the
same owed repositories, across the alias, full repo id, `r_` token, GCP, Flux,
external config, and control config routes. Both callers are covered: the
inbound closure through `addInboundTargetedMaintenanceSources`, and the
per-commit load through `BackfillScopedCatalog`.
`TestAnchorScopedLoadKeepsOwedEvidenceOnPostgres` repeats the comparison on real
Postgres, so `jsonb` text rendering and the escaped `r\_` term are exercised
rather than modelled.

| State | Rows loaded of 49 | Edges of 7 | Noise content rows | Noise file rows |
|---|---|---|---|---|
| Before | 49 | 7 | 20 | 20 |
| Token selection only | 27 | 5 | 0 | 20 |
| After | 29 | 7 | 0 | 20 |

Six production mutants were applied through `go test -overlay`, and each failed
at least one named test.

No-Regression Evidence: the corpus above loads 29 of 49 rows instead of 49, with
all 7 owed edges equal to the fleet answer on the in-memory harness and on
PostgreSQL 18.6. `TestTargetedMaintenanceMatchesWholePass` and its interleaving
and outcome siblings pass unchanged. The statement text is unchanged; only its
term list differs. Control-repository generators now load their own config
files, which adds a bounded read per such repository. In the QA environment 6
repositories hold an ApplicationSet. Their `yaml`, `yml` and `json` rows are
2,348 `content` rows (4,013 kB stored) and 2,348 `file` rows (6,518 kB stored),
at most 1,881 rows in one repository, against about 330,000 rows and 2.5 GB.

Graph truth changes beyond the per-commit fix. The deferred pass and the closure
also gain control-config evidence: on PostgreSQL 18.6 the deferred load went from
6 of 8 evidence edges to 7 of 8 against the fleet answer, gaining a real
`ARGOCD_APPLICATIONSET_TEMPLATE_SOURCE` edge. `golden-corpus-gate-neo4j` is the
required graph gate for that change.

No-Observability-Change: no metric, span, or log changes. The pass keeps its
`relationship.backfill_deferred_targeted` span and duration metric. Neither
reports how many rows the anchor load returned, so that count is still not
visible to an operator. A log at pass start is a separate follow-up.

## Limits

This fixes `content` rows only. Every `file` row carries null
`argocd_applications` and `argocd_applicationsets` keys, so the ArgoCD markers of
this change still match all of them: the 20 noise file rows above.
[9781-argocd-content-markers.md](9781-argocd-content-markers.md) narrows those.

A spelling that percent-encodes the remote's last path segment (`my%2Dapp` for
`my-app`, `caf%C3%A9`) holds neither that token nor any alias, because
`NormalizeRemoteURL` decodes it. This change alone does not anchor those rows; the
file-row markers select them. #9781 replaces those markers with one that selects
every row holding a GitRepository, so the exception stays covered.

## Slug Aliases

Length says nothing about how rare a slug's owner is. One owner covers almost the
whole fleet and sits in the clone path of most `file` rows, so a slug anchored on
it selects those rows through their paths. In the QA environment 29 of 800
repositories had an owner longer than their repository token, and the owner was in
85.3% of a 1% sample of `file` rows.

An alias with a slash now anchors on its last token. A reference spelled as the slug
holds that token, and the name alias usually supplies it already, so the term list
loses the owner and gains nothing. A path that ends in `repository` falls back to
the owner, because the fleet-wide token is worse.

Each repository's anchors, applied to a 0.4% sample of active `content` and `file`
rows, as a share of those rows. The anchors are computed in SQL from the catalog
using the code's rule, so this models the rule rather than a built binary. The
samples are separate reads of 1,444 and 1,426 rows, and the markers' 7 rows are not
in the shares.

| Rows selected | Longest token | Slash alias takes the last |
|---|---:|---:|
| p50 | 0.00% | 0.00% |
| p90 | 0.55% | 0.49% |
| p99 | 41.55% | 2.74% |
| max | 47.58% | 13.81% |
| Repositories at 25% or more | 29 | 0 |
| Repositories at 10% or more | 31 | 2 |
| Repositories at 5% or more | 32 | 5 |

About two of those 5 repositories are a large repository's own rows, reached
through its `r_` token. The others come from a short term, such as a provider name
from a `terraform-modules-*` alias or a repository name of a few characters, that
`LIKE` matches as a substring across the fleet. Those shares grow with the fleet,
so the bound for them is not "one repository". A review read of two QA samples
put the widest of these at 5% to 14% of rows. #9781 tracks the bound. The 100,000 to 43 drop in the
[#9781 note](9781-argocd-content-markers.md) comes from a synthetic table with no
owner in its paths, so on real rows it depends on this rule.

Parity holds on the corpus: a slug reference keeps its edge, and a row holding the
owner only in its clone path stays out, in memory, on PostgreSQL 18.6, and on the
deferred load. Restoring the longest-token rule for slugs, or taking the last token
of every alias, fails those parity tests, not only the unit tests that pin the rule.
