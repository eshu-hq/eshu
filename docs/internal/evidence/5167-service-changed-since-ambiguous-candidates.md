# One granted candidate is not a grant: the ambiguous-correlation hole

`GET /api/v0/freshness/services/changed-since` refuses a catalog `service_id`
that anything outside the caller's grant also claims. The exclusivity fence
that does the refusing is in
[5167-service-changed-since-shared-ownership.md](5167-service-changed-since-shared-ownership.md);
this document covers the second hole review round 3 found in it (PR #6472,
Codex P1-B) and the fix.

## Withdrawn from #6472

The route's promotion was pulled from PR #6472. The clause below ships and is
tested, but neither it nor the exclusivity fence it corrects moves
`GET /api/v0/freshness/services/changed-since` off `pendingRowFilteringRoutes`,
so the route keeps its ledger row and the middleware still answers every scoped
token and browser session with a 403. The reason is the aged-out correlation
under [What is still open](#what-is-still-open); the whole account, including
the three findings that settle why no in-handler rework closes it, is in
[5167-service-changed-since-shared-ownership.md](5167-service-changed-since-shared-ownership.md).
Read the sections below as the fence that landed, not as a route a scoped
caller can reach.

## The defect

The admission arm of `listServiceCatalogCorrelationsQuery`
(`go/internal/query/service_catalog_correlations.go`) matches a correlation
when **any** of these is true: its `repository_id` is granted, any entry of its
`candidate_repository_ids` is granted, or its scope is granted. That is the
right question for admission. It asks whether the caller has some claim on the
row.

`listServiceCatalogCorrelationsOutsideGrantQuery` was that arm with the whole
disjunction negated, which asked the wrong question back. A row listing one
repository the caller owns and one it does not matched the admission arm
through the `?|` any-candidate test, so the plain negation made that same row
count as *inside* for the exclusivity probe. The id stopped looking contested,
and `serviceChangedSinceGrantAdmits`
(`go/internal/query/freshness_service_changed_since.go`) admitted the caller
onto lineage the ungranted repository also claims: current and prior generation
ids and timestamps, per-family counts, and the bounded `service_evidence_key`
samples that embed owner refs, deployment identities, and incident identities.

This is a shape the tables really produce, not a hypothetical:

- The reducer's ambiguous branches (`classifyServiceCatalogEntity` and
  `classifyRepoLocalServiceCatalogEntity`,
  `go/internal/reducer/service_catalog_correlation_classify.go`) never set
  `decision.RepositoryID`. They put every matched repository in
  `decision.CandidateRepositoryIDs` and leave the repository id empty.
- `serviceCatalogBaseDecision` in the same file still fills `ServiceID` and
  `OwnerRef` on that decision.
- `buildServiceOwnershipMaterializations`
  (`go/internal/reducer/service_materialization.go`) needs only a service id
  and an owner ref, so an ambiguous decision is written as a generation exactly
  like an exact one and reaches the same globally keyed lineage.

## The fix

A row is inside only when the grant covers some of its ownership evidence
**and** no candidate repository falls outside the grant:

```text
(repository_id granted OR some candidate granted) AND every candidate granted
```

Both halves are load-bearing, which is the part that is easy to get wrong. The
obvious tightening -- outside when the `repository_id` is not granted, or some
candidate is not granted -- drops the first half, and an ambiguous decision has
no `repository_id` to satisfy. Every service carrying any ambiguity would then
return not-found to the tenant that wholly owns it. The row-truth table below
measures both.

The ordinary statement is untouched, so every other caller of this store keeps
its plan cache entry.
`TestServiceCatalogCorrelationsOutsideGrantQueryDiffersOnlyInTheGrantClause`
still rebuilds the ordinary statement from the inverted one and fails if they
differ anywhere but the grant clause; the pinned clause text is now the
stricter one.

### Query count per scoped request

Counted against the handler, which no scoped caller reaches while the route is
pending: unchanged at **two** correlation queries for a scoped caller whose
grant covers the service, and **one** for a caller whose grant does not. The
exclusivity probe still runs only after the admission probe has already
returned a row. The fix adds no statement, no join, and no round trip. It
changes one WHERE arm inside a statement that already ran. An unscoped caller
still issues zero.

## Row truth

The two clauses evaluated over the same six rows on PostgreSQL 16.15, in a
throwaway container on an Apple-silicon laptop, data-plane schema applied from
`schema/data-plane/postgres` in filename order (53 files, 0 failures),
synthetic rows only -- no real repository, service, owner, or scope name. Grant
under test: `AllowedRepositoryIDs = {repo-aaaa}`, `AllowedScopeIDs = {}`. Every
row sits in a scope outside that grant, so the scope arm never rescues a row
and each verdict comes from the payload alone.

`shipped` is the clause as it stood on head `e4a0697bf`. `fixed` is the clause
that landed. `naive` is the tightening that drops the first half.

| Row | `repository_id` | `candidate_repository_ids` | shipped | fixed | naive |
| --- | --- | --- | --- | --- | --- |
| mixed ambiguous | *(none)* | `["repo-aaaa","repo-bbbb"]` | inside | **outside** | outside |
| all candidates granted | *(none)* | `["repo-aaaa"]` | inside | inside | **outside** |
| exact, granted | `repo-aaaa` | *(none)* | inside | inside | inside |
| exact, ungranted | `repo-bbbb` | *(none)* | outside | outside | outside |
| no ownership evidence | *(none)* | *(none)* | outside | outside | outside |
| granted plus ungranted candidate | `repo-aaaa` | `["repo-aaaa","repo-bbbb"]` | inside | **outside** | outside |

Three things this settles. Row 1 is the defect: the shipped clause called it
inside, so the exclusivity probe returned nothing and the caller was admitted;
the fixed clause reports it and the handler refuses. Row 2 is why the naive
tightening is wrong -- it marks a row every one of whose candidates is inside
the grant as outside, refusing the caller's own service. Rows 3 through 5 are
identical under all three clauses, so the arm that reads candidates does not
disturb the unambiguous shapes.

Row 6 is a deliberate choice rather than a forced one. A resolved
`repository_id` sitting next to a leftover ungranted candidate is treated as
contested. The candidate list is evidence that another repository claimed this
entity, and the fence exists because the lineage cannot be split at all, so the
fail-closed direction is the one that cannot hand over another tenant's
evidence.

The ordinary admission arm was evaluated in the same query and matches rows 1,
2, 3, and 6. Rows 1 and 6 are therefore admitted by the first probe and refused
by the second, which is the intended two-probe shape.

## Performance evidence

`EXPLAIN (ANALYZE, BUFFERS)` on the candidate statement, run before the
production code was written, in the environment described above. Bound through
`PREPARE` plus `EXPLAIN ... EXECUTE` after five warm executions, mirroring the
pgx `cache_statement` default the store runs under. Data: two scopes with one
active generation each, the six correlation rows above, plus 10,000 bulk
correlations split across the two scopes. 10,006 `fact_records` rows, `ANALYZE`
after the load. Non-comparable to any reference profile, and no absolute target
is claimed.

| Shape | Rows | Plan | Execution | Buffers | Planning |
| --- | ---: | --- | ---: | --- | ---: |
| Mixed ambiguous id, tenant A's grant | 1 | Index Scan using `fact_records_service_catalog_correlations_service_idx` | 0.054 ms | shared hit=7 | 2.485 ms |
| All-candidates-granted id, same grant | 0 | same index | 0.021 ms | shared hit=7 | 0.643 ms |
| Single-owner granted id, same grant | 0 | same index | 0.020 ms | shared hit=7 | 0.626 ms |

`Index Cond: ((payload ->> 'service_id') = ...) AND (generation_id =
scope.active_generation_id)` on every shape. Same index, same cardinality, and
the same `shared hit=7` the shipped inverted statement was measured at in the
sibling document. The new containment test lands in the Filter, not the Index
Cond, so it costs no extra buffer reads and changes no plan.

Planning still dominates execution by one to two orders of magnitude, the same
profile the sibling document recorded. The first row's higher planning time is
the cold plan for a freshly prepared statement; the two rows below it are the
warm figure.

No-Regression Evidence: the ordinary statement's text, bind arguments, joins,
and index are unchanged, so its plan cache key is byte-identical to
`origin/main`, and two tests pin that. The inverted statement keeps the same
index, row count, and buffer count it had before this change. No new query is
issued on any path, scoped or unscoped.

No-Observability-Change: the refusal already records
`eshu.service_changed_since.grant_refused` and the reason `shared_ownership` on
the handler span, through `refuseServiceChangedSinceGrant`
(`go/internal/query/freshness_service_changed_since.go`). An ambiguous
correlation naming an ungranted repository is the same refusal for the same
reason -- two owners contesting one id -- so it reports through the existing
attributes, and the closed reason vocabulary in
`go/internal/telemetry/contract_zzzz_service_changed_since.go` is unchanged. No
metric and no log key is added, so no
`docs/public/observability/telemetry-coverage.md` row is owed.

## Red, then green

`TestServiceChangedSinceAmbiguousCandidateOutsideTheGrantIsRefused`
(`go/internal/query/freshness_service_changed_since_ambiguous_test.go`) is new.
The ownership fake now mirrors each statement's own grant arm instead of
assuming the two are complements.

Written first, against the shipped clause:

```text
$ go test ./internal/query -run 'ServiceChangedSinceAmbiguous' -count=1; echo "EXIT=$?"
EXIT=1
--- FAIL: TestServiceChangedSinceAmbiguousCandidateOutsideTheGrantIsRefused
    --- FAIL: .../one_ungranted_candidate_refuses_the_whole_row
        status = 200, want 404; an ambiguous row naming an ungranted candidate
        must not resolve; body = {"data":{...,"current_active_generation_id":
        "gen-current-shared","service_id":"component:default/api",...}}
```

That red is the defect: tenant A's scoped token, 200, carrying the contested
lineage's active generation id, reached through a row tenant A only partly
owns. The second subtest passed in the same run, which is what makes it a guard
against over-refusal rather than a restatement of the first.

Green after the clause landed:

```text
$ go test ./internal/query -run 'ServiceChangedSince|ServiceCatalogCorrelation' \
    -count=1; echo "EXIT=$?"
ok  	github.com/eshu-hq/eshu/go/internal/query	1.240s
EXIT=0
```

## BITES

Each mutation applied alone, run, reverted. Exit codes captured directly.

| Mutation | Case that fails | Exit |
| --- | --- | --- |
| Shipped statement reverted to the plain negation of the admission arm | `TestServiceCatalogCorrelationsOutsideGrantQueryDiffersOnlyInTheGrantClause`: the pinned clause text is no longer in the statement | 1 |
| Ownership fake treats the two statements as complements again | `one ungranted candidate refuses the whole row`: 200 with `gen-current-shared` | 1 |
| Fake drops the some-evidence-granted half (the naive tightening) | `an ambiguous row whose candidates are all granted still resolves`: 404 for the caller's own service | 1 |
| Shipped code | pass | 0 |

The first row is the one the handler tests cannot see. They drive a fake, so a
statement that quietly went back to the plain negation would keep every handler
assertion green while production admitted every partly-granted ambiguous id.
The second and third rows are the reverse: they pin that the fake stays honest
to the statement it mirrors.

## Contract text

This section is history. Commits seven through ten put an admission sentence on
four surfaces, and commit ten gave it the clause below, kept identical in all
four: the OpenAPI operation description
(`openAPIPathsFreshnessServiceChangedSince`,
`go/internal/query/openapi_paths_service_changed_since.go`), the
`get_service_changed_since` tool description
(`go/internal/mcp/freshness/tools.go`),
`docs/public/reference/http-api/status-admin.md`, and the tool's row in
`docs/public/reference/mcp-tool-contract-matrix.md`. It read: scoped tokens
receive a service only when every repository with a currently active catalog
correlation for it is in the grant, **including every candidate repository of a
correlation that matched more than one**.

The promise itself did not change. A candidate repository was always "a
repository with a currently active catalog correlation for it". What changed is
that the query keeps that promise for ambiguous rows, where before it did not.
The added clause made the promise checkable by a reader who has not read the
SQL.

The withdrawal commit then took the whole admission sentence off all four
surfaces along with the promotion, and what ships is the refusal.
`openAPIPathsFreshnessServiceChangedSince` says scoped tokens are refused with
a 403 "in every deployment, and so is every browser session except a
tenant-bound all-scope console session", because the service lineage tables
carry no column naming the tenant a row belongs to, and that `local_no_policy`
and `hosted_single_tenant` admit that console session "as it does on every
route outside the scoped-token allowlist". The `get_service_changed_since`
definition says the same in its own space. The clause above comes back with the
promotion once #6475 lands.

`TestToolsPreserveFreshnessRegistrationContract` pins a SHA-256 over the
marshalled freshness tool definitions. The candidate clause moved that pin from
`d1349562...` to `eb23a5e1...` while it stood; the withdrawal and the 403
wording moved it to `ca92b326...`, and naming the admitted browser session
moved it again, to `74856c94...`. Neither a
cassette (the recorded collector responses the golden-corpus gate replays) nor
a B-12 snapshot entry carries tool or operation description text, so nothing is
regenerated.

## What is still open

This closes the ambiguous-candidate hole. It does not close the aged-out one
described under "The liveness gap" in the sibling document: both correlation
statements still require the fact's generation to be its scope's active one, so
a correlation that has aged out is invisible to either probe while its lineage
generation stays the globally active one for the id. That remains tracked as
[#6475](https://github.com/eshu-hq/eshu/issues/6475).

Binding admission to the lineage rows themselves was investigated as a way to
close it here, and it does not work on committed state. `ownershipEvidencePayload`
(`go/internal/reducer/service_materialization.go`) builds each
`service_evidence_snapshots` ownership row from five fields -- `owner_ref`,
`provider`, `entity_ref`, `lifecycle`, `tier` -- and reads neither
`decision.RepositoryID` nor any scope id, so the repository is dropped one call
before the write. `buildServiceOwnershipMaterializations` is the only
production producer of a `ServiceMaterializationWrite`, and
`commitServiceGenerations`
(`go/internal/reducer/service_catalog_correlation.go`) its only caller, so no
second path fills ownership differently. Every generation does carry at least
one ownership row, because that builder emits a write only for a decision that
has both a service id and an owner ref. It is the row contents, not their
presence, that fails.

The one family whose payload does carry a repository is deployment:
`serviceDeploymentEvidencePayload`
(`go/internal/reducer/service_materialization_deployment.go`) writes
`source_repo_id` and `target_repo_id`. It is not a usable fence. Those rows
exist only when a deployment relationship loader is wired and the service's
repository has resolved relationships, so an ownership-only generation has
none, and a fence that admits whenever the family is absent is not a fence.
Runtime and docs payloads carry no repository id at all.

So #6475 -- a scope column on both lineage tables plus a `(scope, service_id)`
writer key -- remains the only real fix, now confirmed from the writer side
rather than only from the schema side.
