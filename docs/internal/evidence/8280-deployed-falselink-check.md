# #8280 deployed false-link check (QA, 2026-10-19)

Hand-check of 10 NEW cross-repository `CALLS` edges with
`resolution_method=import_binding` on the QA environment, read at the exact
active content generations. Result: 10 true, 0 false. The interim set of 10
(issue comment 2026-10-17T21:32:08Z) is disjoint from this set.

## Wave state

- 39/39 `repository_reindex_requests` rows present: 35 at
  `2026-10-17T20:55:15.821189Z`, 4 at `2026-10-17T21:23:22.824324Z`.
- 39/39 scopes carry an active full (`is_delta=false`) generation ingested
  after their `requested_at` (batch 1 `20:58-21:14Z`, batch 2 `21:25:27Z`;
  `portal-nextjs-platform` has since advanced to a full `2026-10-19T13:30Z`
  generation from the scheduled sweep).
- Fleet image at wave time (`sha-15734d0`, rolled `2026-10-17T20:45:21Z`) is a
  descendant of both the #8314 (#8280 shapes) and #8337/#8310 (closed plain
  rule, `git-collector.v8`) merges, so the wave parsed at v8.
- Stored v8 output in the 39 active generations: 17 JS static-member
  (`Export.method`), 3147 PHP instance (`FQCN->method`), 6321 other packaged
  producer keys; 0 `function:` keys.
- Queue: `code_call_materialization` 79 succeeded / 0 outstanding,
  `code_import_repo_edge` 78 succeeded + 1 superseded. Only unrelated
  `container_image_identity` (9 pending + 1 claimed) and
  `deployment_mapping` (11 pending) items remain open.

## Method

Per edge: caller and callee files read in full from `content_files` at the
node generations; import binding, declared manifest/lock dependency, single
call site, producer declaration, and deployment-wide publisher uniqueness of
the full `package#export` key each confirmed.

## JS/TS: 5 new, 0 false

- T1 `portal-nextjs-platform` `apps/bff-api/src/datasources/forex/pinned-rate-reader.ts`
  `createPinnedForexRateReader` -> `lib-node-caching`
  `src/cache-clients/factory.ts` `createClient` (STATIC-MEMBER, new shape).
  Bare named import of `CacheClientFactory` from `@dmm/lib-node-caching`
  (declared `apps/bff-api/package.json` `1.5.1`); single call
  `CacheClientFactory.createClient({...})`; producer declares
  `export class CacheClientFactory` with `static createClient`; key
  `@dmm/lib-node-caching#CacheClientFactory.createClient` has 1 publisher
  deployment-wide. Consumer gen `bc9ac425` (post-sweep generation; the
  wave-time portal generation has since been superseded),
  producer gen `0f14f386`. TRUE.
  This is the interim source-verified edge, now materialized.
- T2 `api-node-boattrader` `server/handlers/app/blocks.ts` `getBlocks` ->
  `lib-node-blocks` `src/index.ts` `getBlock`. Destructured
  `require('@dmm/lib-node-blocks')`; single call
  `getBlock(hostname, code, contentType, locale)`; producer declares
  `export const getBlock`; key unique (1 publisher). Consumer gen `83a22cd9`,
  producer gen `bb0e2333`. TRUE.
- T3 `portal-nextjs-platform`
  `apps/bff-api/src/datasources/boat-direct-datasource.ts` `getBoat` ->
  `lib-node-search` `src/actions/getById.ts` `getById`. Bare named import
  (declared `12.10.1`); single value call at line 647; other mentions are
  comments or a `Parameters<typeof getById>` type position; producer declares
  `export const getById`; key `@dmm/lib-node-search#getById` unique
  (same export name under 3 other packages does not collide: the key carries
  the package). Consumer gen `bc9ac425` (post-sweep generation; the wave-time
  portal generation has since been superseded), producer gen `455bed10`. TRUE.
- T4 `lib-node-search` `src/builders/filterTermForMake.ts`
  `filterTermForMake` -> `lib-node-make-model` `src/resources/elastic.js`
  `getMake`. Bare named import (declared `^1.9.0`); single call
  `getMake(services.elasticSearchClient, trimmed)`; producer declares
  `export const getMake`; key unique. Consumer gen `455bed10`,
  producer gen `b63544df`. TRUE.
- T5 `api-node-boattrader` `server/resources/ads/adHelper.ts`
  `serveRoadBlocks` -> `lib-node-ads` `src/index.ts` `serveRoadblocks`.
  Aliased named import `serveRoadblocks as serveRoadblocksLib` (declared
  `^7.0.4`); single call; producer declares `export const serveRoadblocks`;
  key unique. Consumer gen `83a22cd9`, producer gen `51486c30`. TRUE.

## PHP: 5 new, 0 false (all instance-receiver, new shape)

- P1a/b/c `theme-essential-elite`
  `src/Helper/Common/AdminSesHelper.php` `run` -> `lib-php-common`
  `src/Service/Ses/SesApi.php` `send` / `setText` / `setUpEmail`.
  `$ses = new SesApi(...)` written once, then member calls/reads only
  (`$ses->setUpEmail`, `$ses->setText`, `$ses->send`); producer declares
  top-level `class SesApi` with all three `public function` bodies;
  keys `dmm-cw/lib-php-common#<FQCN>-><method>` unique (1 publisher each).
  Consumer gen `e2f60cf0`, producer gen `70158ef5`. TRUE x3.
- P4 `theme-essential-elite`
  `src/Helper/Engines/EngineSearchHelper.php` `resizeImage` ->
  `lib-php-common` `src/Helper/Utilities/ImageHelper.php`
  `getResizedImtImageUrl`. `$imageHelper = new ImageHelper()` then one
  member call; producer declares top-level `class ImageHelper` with the
  `public function`; key unique. Consumer gen `e2f60cf0`,
  producer gen `70158ef5`. TRUE.
- P5 `plugin-cobrokerage` `includes/lib/class-cobrokerage-widget.php`
  `widget` -> `lib-php-common`
  `src/View/ViewModel/Factory/CobrokerageVmFactory.php`
  `newCobrokerageViewModel`. `$vm_factory = new CobrokerageVmFactory()`
  then `$vm_factory->newCobrokerageViewModel(...)`; producer declares the
  `public function`; key unique. Consumer gen `d5ac8115`,
  producer gen `70158ef5`. TRUE.

Both consumers lock `dmm-cw/lib-php-common` (`theme-essential-elite`
`composer.lock` pins `2.9.0`; `plugin-cobrokerage` lock references it).

## PHP `use function`: 0 deployed edges

`content_files` across the 39 reindexed repositories (14,118 rows)
holds zero `use function` statements. Reproducible predicate, scoped to the
reindexed repos:

```sql
SELECT count(*) FROM content_files
WHERE repo_id IN (SELECT replace(scope_id, 'git-repository-scope:', '')
                  FROM repository_reindex_requests)
  AND content_lower ~ 'use[[:space:]]+function'
  AND language = 'php';
-- 0 rows. Unscoped regex finds 1 wrap artifact ("pause\nfunction perOpMicros"
-- in a TS bench spec), disposed by the language filter; a statement-anchored
-- variant also returns 0.
```

So no deployed edge of that shape can exist. That subset rests on fixture
proof, as recorded pre-deploy. No third reindex batch was requested
(cap 39/100 holds).

## Docs and capability matrix

No change: the 0-false-link result confirms the shipped rows; nothing new is
claimed.
