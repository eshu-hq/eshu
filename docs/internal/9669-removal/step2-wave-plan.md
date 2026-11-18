# Step-2 execution plan: core forwarded serving surface (2026-11-18)

Parent ruling: [9669-step2-scope-ruling-2026-11-18.md](../arbiter/9669-step2-scope-ruling-2026-11-18.md).
Pins: core `88f1fd2a22`, satellite `08ebde4` (v0.6.0), oracle `8a78eb3f48`.

## Frozen scope formula

`228`-read census (`docs/parity.md:9-16` at `08ebde4`: 278 live = 228
read + 50 mutation; `newRouter` 247 + `wireAPI` 31) **minus** ~221
terminated viaNative census reads **plus** 19 core-owned survivor
mutations (tier-C dispatch clones, zero removal credit).

Forwarded remainder ≈ 7 relay-rider reads (dependencies/code-bundle
leftovers per D4/D6 disclosures, repo/docs freshness path reads,
content list/scoped-entity legs, out-of-pin search legs, plus
trailing-slash/non-GET twins via the `router.go:483` catch-all) + 19
mutations (d7 4: backfill/dead-letter/reindex/skip; setup 3:
claim/admin/mfa; recovery 4: replay/reopen/refinalize/recover-generations;
signin PATCH 1; d9 login 7: OIDC 2 + GitHub 2 + SAML 3).

Scope artifact: `contracts/step2-frozen-scope.md` (or `.json`) with
Method+Path+Family+oracle fragment + satellite wave entry line per row.
Gate: row count == census minus terminated + 19. Rows not on the list
are out; rows on it are in.

## Contract tiers (build, don't rebuild)

- Tier (a) EXISTS at v0: `TestCoreReadContractsVersioned` (GraphQuery 2
  + ContentStore 36), `TestReadStoreContractIsVersioned` (db.ReadStore
  3), ~83 per-family `ContractIsVersioned`, G2 canned-pin wire tests.
- Tier (b) PARTIAL: `contracts/` holds 10 `.md`; mint the rest
  core-side (precedent: `contracts/code-flow-read-admission.md`).
- Tier (c) EXISTS: `CoreReadContractsVersion = v0` move signal.
- Tier (d) ABSENT: mint `contracts/upstream/<family>-read-wire-shapes.txt`
  via generator `scripts/gen-read-wire-shapes.sh` (snapshots from
  `8a78eb3f48` fragments; never hand-edited).

## Waves

- **Wave 0** — scaffolding + oracle pin (first PR): scope file,
  `contracts/upstream/README.md` + generator, G2 stubs
  (ExtractionReadiness 2 / FactSchemaVersions 2 / SurfaceInventory 1),
  docs fix for stale 66/278 prose. No new pins (template green).
- **Wave 1** — G2 seven-leg replication per row (uses
  `g2_read_admission_wire_test.go` canned-pin proofs as reference).
- **Wave 2** — Status 18 (largest tier-b gap): mint
  `contracts/status-*-read-admission.md` + contract tests + stubs.
- **Wave 3** — SupplyChain 19 + Compare 1 + ProviderConfig 3 (+3 reads).
  Note: `POST impact/suppressions` is the sole bounded mutation via
  `SuppressionAdmission` — stays out of the read seal.
- **Wave 4** — Code 27 + Language 1 + Content 7 + content-search.
  Preserve codeGraphWave ownership of
  `POST /api/v0/code/call-graph/metrics` (W8a must never absorb).
- **Wave 5** — remainder families in oracle order (Entities 9,
  Impact 13, IaC 11, Infra 9, Repositories 12, SecretsIAM 5,
  PackageRegistry 7, Documentation 6, Evidence 5, …), one PR per
  family-group. Auth/mutation families are out except their read halves
  (AdminIdentityReads 8, ProviderConfigReads 3, SignInPolicyReads 2,
  Setup `GET setup-state`).

## Per-row seven-leg checklist (W8a replication)

1. Conformance (frozen Method+Path+Family+gated+Fragment, ahead of
   relay, slash twins native, byte-intact forward + correlation echo).
2. Auth (shared-key terminates; scoped/OIDC/session-cookie relay paths
   explicit with 1 byte-identical upstream hit; junk/none 401 zero hits;
   deny 403 zero contact).
3. Fail-closed (`nil`-core 503 no `Retry-After` both lanes; deny 403;
   empty-key 401).
4. Parity-GET (native-vs-relay byte identity across the status matrix ×
   2 envelopes, filter-query intact).
5. Parity-POST (same matrix + upstream-body equality + family wire shapes).
6. Latency (stub-core benchmark vs relay benchmark, bound 1.50x,
   `scripts/measure-*.sh`).
7. Spec (disclosed-key count == wave length; native `openapi.json`
   omits wave keys, stays 200 valid JSON).

Plus lane rule in `*_terminate.go` and the rollback line (remove
`register*Wave` + re-add routes to relay disclosures, no data migration).

## Measurable done (ruling decision 8)

- Satellite `make ci` green with the new core as `--core-base-url`.
- Core floor green = `make pre-push` on the step-2 worktree.
- Every frozen-list row's seven-leg suite green, enumerated by name.
- `curl /readyz` → 200 with live Postgres/Neo4j, 503 with either down
  (both demonstrated). Latency bound: satellite `dispatchTimeout` 30s /
  `readclient.DefaultTimeout`, pinned equal by test.

## Open items (owned)

- S2 (19-mutation parent definition): with arbiter — candidate
  4+3+4+1+7 vs coincidental 50−31 set. Blocks the scope file's mutation
  table; forwarded-reads work proceeds regardless.
- C1/C2 decided coordinator-side: tier-b docs + tier-d generator live
  core-side (`contracts/`, `scripts/`) per in-repo precedent.
- C3 (81-vs-83/84 skew), O4 (stale 66/278 prose): folded into Wave 0.
