# Step-2 frozen scope: forwarded reads (Wave 0, 2026-11-18)

Refs `eshu-hq/eshu#9669`, satellite `eshu-hq/eshu-api#101`.
Parent plan: `docs/internal/9669-removal/step2-wave-plan.md`.
Ruling: `docs/internal/arbiter/9669-step2-scope-ruling-2026-11-18.md`.

Pins: core oracle `8a78eb3f48`, satellite origin/main `08ebde4`
(v0.6.0), lane base `88f1fd2a22`. Fragments below are relative to
`go/internal/query/openapi/` at the oracle commit.

## Census (mechanical, oracle `8a78eb3f48`)

Commands (run anywhere with the core repo):

```bash
git show 8a78eb3f48:go/cmd/api/CUTOVER_OWNERSHIP.md | rg -c '^\| `'   # 282 rows
git show 8a78eb3f48:go/cmd/api/CUTOVER_OWNERSHIP.md | rg '^\| `' | awk -F'|' '{print $3}' | sort -u | wc -l  # 67 families (see note)
```

Result: **282 rows, 67 families: 231 read + 51 mutation.** The family
count above is illustrative; the authoritative count (67) was taken
with a strict cell split (family is the second table cell). Channel
split (fourth cell): 231 `read`, 51 `mutation`. The 231 reads are 228
API reads plus 3 non-API reads (`GET /api/v0/docs`, `GET /api/v0/redoc`,
`GET /health`: `<doc-ui>`/`<mux-health>` rows).

This supersedes the stale `278 live = 228 read + 50 mutation`
prose (satellite `docs/parity.md:9-16`, recorded before the
downstream-consumers, manifest-dependencies, api-surface, and
retire-unclaimed additions).

## Satellite served set (`08ebde4`)

`openapi/spec_snapshot.json` at `08ebde4`: 284 mounted, 279 served,
5 by-design non-API exclusions (pinned by `TestSpecScope_Frozen`).
Diffed mechanically against the oracle census: every one of the 279
API census rows is natively registered and served; the only census
rows outside the served set are the 3 non-API rows above, which the
satellite answers dest-side (doc shells, probe) and never forwards.

## Reconciliation (measured, not estimated)

With row-7 (`#153`) and G2 (`#152`) landed at the pin, **zero
base-spelling census read rows remain on the generic relay**: every
record-first pin that once disclosed a relay posture
(`metrics-read-admission.md`, `semantic-search-read-admission.md`,
`terraform-drift-read-admission.md`, the D6 relay-only set now
pinned intentionally empty) has since terminated natively, and the
served snapshot proves it row-for-row.

The residual forwarded-read surface is therefore the
non-registered-spelling class crossing the `api/router.go:483`
`/api/` catch-all, which the satellite forwards and core judges
(core allowlists each route to its pinned method). The table below
pins each evidenced rider spelling to its base census row. Six of
the seven resolve to base rows inside the natively-served set; that
is expected — a rider is a spelling, not a new route — and the gate
below counts rider rows, not new base rows. Stale latency-test
comments that still call natively-served routes (e.g. `POST
/api/v0/impact/change-surface`, terminated in satellite W3 `#151`)
"relay legs" describe minimal bench muxes, not the production mux,
and anchor no row here.

## FORWARDED-READS (7 rows, frozen)

| # | Relay-crossing spelling | Base row | Family | Oracle fragment | Satellite wave entry line (`08ebde4`) |
| --- | --- | --- | --- | --- | --- |
| R1 | `PATCH /api/v0/code/bundles` (wrong-method twin) | `POST /api/v0/code/bundles` | Code | `go/internal/query/openapi/paths/code/graph.go` | `api/signinpolicy_admin_latency_test.go:115-117` ("PATCH /api/v0/code/bundles, which stays on the generic relay") |
| R2 | `GET /api/v0/auth/sign-in-policy/` (slash twin) | `GET /api/v0/auth/sign-in-policy` | SignInPolicyReads | `go/internal/query/openapi/paths/auth/sign_in_policy.go` | `api/router.go:437-440` + `:453-455` ("twins stay on the generic relay"; keyed callers get core's answer for the spelled path) |
| R3 | `GET /api/v0/auth/providers/` (slash twin) | `GET /api/v0/auth/providers` | AuthProviders | `go/internal/query/openapi/paths/auth/routes.go` | same `api/router.go` lines as R2 |
| R4 | `POST /api/v0/freshness/changed-since` (keyed; 401 keyless) | `GET /api/v0/freshness/changed-since` | Freshness | `go/internal/query/openapi/paths/freshness/changed_since.go` | `contracts/freshness-wave.md:112-114` ("a keyed POST crosses the generic relay and core judges the method") |
| R5 | `POST /api/v0/freshness/generations` (keyed; 401 keyless) | `GET /api/v0/freshness/generations` | Freshness | `go/internal/query/openapi/paths/freshness/generations.go` | same `contracts/freshness-wave.md` lines as R4 |
| R6 | `POST /api/v0/freshness/services/changed-since` (keyed; 401 keyless) | `GET /api/v0/freshness/services/changed-since` | Freshness | `go/internal/query/openapi/paths/freshness/service_changed_since.go` | same `contracts/freshness-wave.md` lines as R4 |
| R7 | `POST /api/v0/surface-inventory` (keyed wrong-method; 401 keyless) | `GET /api/v0/surface-inventory` | SurfaceInventory | `go/internal/query/openapi/paths/catalog/surface_inventory.go` | `contracts/g2-wave.md:131-134` ("a keyed wrong-method call crosses the generic relay and core judges the method") |

Notes:

- R2/R3 deeper subtrees (e.g. `/api/v0/auth/providers/...`) ride the
  same catch-all but are not rows; no spec key exists for them on
  either side.
- R7 is the Wave-0 SurfaceInventory stub's rider: the stub must
  preserve core's method judgment for the spelled path. The other
  six G2 paths' wrong-method twins belong to Wave 1's seven-leg
  replication, not to this table.
- R1/R4-R6 riders attach to Code/Freshness lanes owned by later
  waves (Waves 3/5); the riders are pinned here so the core service
  answers them byte-identically from Wave 0 on.

## Mutations (19 rows, frozen per S2 verdict)

S2 SETTLED (candidate (A) **4+3+4+1+7 wins**; 50−31 rejected as a
mount/capability category error). Row 17 (`GET .../saml/.../metadata`)
is a census read riding the D9-SAML lane as an indivisible triple: 18
mutations + 1 rider, same 19-row set. Oracle fragments below are
relative to `go/internal/query/openapi/` at `8a78eb3f48`; satellite
lines at `08ebde4`.

| # | Route | Family | Oracle fragment | Satellite line |
| --- | --- | --- | --- | --- |
| M1 | `POST /api/v0/admin/backfill` | Admin | `paths/status/admin.go` (backfill row: 200 400 500 503) | `api/d7.go:36-39`, `contracts/d7-admin-four.md:4-6,156-158` |
| M2 | `POST /api/v0/admin/dead-letter` | Admin | `paths/status/admin.go` (dead-letter row: 200 400 500 503) | `api/d7.go:36-39`, `contracts/d7-admin-four.md` |
| M3 | `POST /api/v0/admin/reindex` | Admin | `paths/status/admin.go` (reindex row: 202 400 500 503) | `api/d7.go:36-39`, `contracts/d7-admin-four.md` |
| M4 | `POST /api/v0/admin/skip` | Admin | `paths/status/admin.go` (skip row: 200 400 409 500 503) | `api/d7.go:36-39`, `contracts/d7-admin-four.md` |
| M5 | `POST /api/v0/auth/setup/claim` | Setup | `paths/auth/setup.go` (setup/claim row: 200 400 401 410 503) | `api/setup.go:23-25`, `contracts/setup-admission.md:4-5,68-70` |
| M6 | `POST /api/v0/auth/setup/admin` | Setup | `paths/auth/setup.go` (setup/admin row: 200 400 401 410 503) | `api/setup.go:23-25`, `contracts/setup-admission.md` |
| M7 | `POST /api/v0/auth/setup/mfa` | Setup | `paths/auth/setup.go` (setup/mfa row: 200 400 401 410 503) | `api/setup.go:23-25`, `contracts/setup-admission.md` |
| M8 | `POST /api/v0/admin/replay` | Admin | `paths/status/admin.go` (replay row: 200 400 403 409 422 500 503) | `api/recovery.go:27-30`, `contracts/recovery-admission.md:4-5,104-107` |
| M9 | `POST /api/v0/admin/reopen` | Admin | `paths/status/admin.go` (reopen row: 200 400 403 404 409 422 500 503) | `api/recovery.go:27-30`, `contracts/recovery-admission.md` |
| M10 | `POST /api/v0/admin/refinalize` | Admin | `paths/status/admin.go` (refinalize row: 200 400 409 500 503) | `api/recovery.go:27-30`, `contracts/recovery-admission.md` |
| M11 | `POST /api/v0/admin/recover-generations` | Admin | `paths/status/admin.go` (recover-generations row: 200 400 403 409 500 503) | `api/recovery.go:27-30`, `contracts/recovery-admission.md` |
| M12 | `PATCH /api/v0/auth/admin/sign-in-policy` | SignInPolicyMutations | `paths/auth/sign_in_policy.go` (sign-in-policy row: 200 400 403 500 503) | `api/signin.go:17`, `contracts/sign-in-admission.md:4` + W7-writers line 55 |
| M13 | `GET /api/v0/auth/oidc/login` | OIDCLogin | `paths/auth/routes.go` (login row: 302 400 401 500 503) | `api/d9.go:37-38`, `contracts/d9-oidc-login.md:46-49` |
| M14 | `GET /api/v0/auth/oidc/callback` | OIDCLogin | `paths/auth/routes.go` (callback row: 201 303 400 401 403 500 503) | `api/d9.go:37-38`, `contracts/d9-oidc-login.md` |
| M15 | `GET /api/v0/auth/github/login` | GitHubLogin | `paths/auth/routes.go` (login row: 302 400 401 500 503) | `api/d9github.go:38-39`, `contracts/d9-github-login.md:47-50` |
| M16 | `GET /api/v0/auth/github/callback` | GitHubLogin | `paths/auth/routes.go` (callback row: 201 303 400 401 403 500 503) | `api/d9github.go:38-39`, `contracts/d9-github-login.md` |
| M17 | `GET /api/v0/auth/saml/providers/{provider_id}/metadata` | SAML | `paths/auth/routes.go` (metadata row: 200 404 500 503) | `api/d9saml.go:39`, `contracts/d9-saml-login.md:54-61` — census read, lane rider |
| M18 | `GET /api/v0/auth/saml/providers/{provider_id}/login` | SAML | `paths/auth/routes.go` (login row: 302 404 500 503) | `api/d9saml.go:40`, `contracts/d9-saml-login.md` |
| M19 | `POST /api/v0/auth/saml/providers/{provider_id}/acs` | SAML | `paths/auth/routes.go` (acs row: 201 303 400 401 500 503) | `api/d9saml.go:41`, `contracts/d9-saml-login.md` |

## Wave-0 G2 stub record

The five Wave-0 stub legs exist and admit through their pins, proven
by the standing canned-pin suite (all green this lane, see lane
report): `CollectorExtractionReadinessHandler` (2 legs) through
`ExtractionReadinessReadAdmission v0`,
`TestCollectorExtractionReadinessServesThroughPin`;
`FactSchemaVersionHandler` (2 legs) through `FactSchemaReadAdmission
v0`, `TestFactSchemaVersionServesThroughPin`; `SurfaceInventoryHandler`
(1 leg) through `SurfaceInventoryReadAdmission v0`,
`TestSurfaceInventoryFailsClosedThroughPin` — all in
`go/internal/query/g2_read_admission_wire_test.go`, with frozen
method sets in the three sibling `*_read_admission_contract_test.go`
files. No new pins; template green.

## Gate

Row count == census reads minus natively-registered base spellings,
counted as rider rows, plus 19: **7 + 19**. Rows not on these lists
are out; rows on them are in. Any base-commit move, satellite
re-pin, or added rider spelling voids this file until re-proven.
