# Evidence — DB-backed OIDC login: workspace resolution (#5040) + reserved-word alias fix (#5042)

## What changed

Two root-cause fixes to the DB-backed OIDC login flow, both in the
`internal/storage/postgres` hot package.

1. **#5040 — login-start resolved a blank `workspace_id` (503).** DB-backed
   OIDC provider configs (`identity_provider_configs`) are tenant-scoped and
   carry no `workspace_id` column, unlike env-file providers. Login-start still
   has to write a non-blank `workspace_id` into `identity_oidc_login_states`
   (`workspace_id TEXT NOT NULL`), so a blank value failed the insert and the
   caller saw an opaque 503. New `PrimaryWorkspaceForTenant`
   (`tenant_workspace_grants_primary.go`, backed by
   `listActiveWorkspaceIDsForTenantQuery`) resolves the tenant's single active
   workspace, **failing closed** — `ErrTenantWorkspaceAmbiguous` when a tenant
   has more than one active workspace (the caller must then require an explicit
   `workspace_id`, never silently pick one) and `ErrTenantWorkspaceNotFound`
   when it has none. The resolver maps both to `ErrOIDCLoginInvalidRequest`
   (400), not 503.

2. **#5042 — reserved-word SQL alias broke all permission-grant resolution.**
   `resolveIdentityAPITokenPermissionsQuery` aliased `identity_role_grants` as
   `grant`, a Postgres reserved keyword used unquoted, so every prepare/execute
   failed at the parser with `syntax error at or near "grant" (SQLSTATE 42601)`.
   `resolvePermissionGrantsForRoles` is the single source of truth for both
   scoped-token issuance and browser-session issuance, and the DB-OIDC callback
   resolves group grants through it, so no DB-backed OIDC group-grant login
   could complete. Renamed the alias to the non-reserved `role_grant`.

## Backend / version / input shape

- Backend: Postgres data plane (the auth-e2e stack's Postgres; NornicDB is not
  on this path — these are relational identity tables).
- `#5042` alias rename touches only the table-alias token in one `SELECT`; the
  column set, predicates, join, and ordering are byte-identical.
- `#5040` adds exactly one bounded read per login-start on the low-cardinality
  control-plane `workspaces` table (one workspace per tenant in the default
  model); callers pass `LIMIT 2`, since only the 0/1/2+ distinction matters.

## No-Regression Evidence:

Measured on the live auth-e2e Postgres (`docker exec eshu-e2e-auth-postgres-1`).

- **#5040 workspace-list read** — `EXPLAIN (ANALYZE, BUFFERS)` of
  `listActiveWorkspaceIDsForTenantQuery` for `tenant_id='default'`, `LIMIT 2`:

  ```
  Limit  (cost=0.29..4.73 rows=1) (actual time=0.016..0.016 rows=1 loops=1)
    Buffers: shared hit=4
    ->  Nested Loop
          ->  Index Only Scan using workspaces_active_idx on workspaces w
                Index Cond: (tenant_id = 'default')   Heap Fetches: 1
          ->  Index Only Scan using tenants_active_idx on tenants t
                Index Cond: (tenant_id = 'default')   Heap Fetches: 1
  Execution Time: 0.042 ms
  ```

  Two index-only scans under a nested loop, terminal `LIMIT 2`, 4 shared-buffer
  hits, 0.042 ms. This is a new but bounded per-login-start read on a table that
  holds one row per tenant; it cannot scan a tenant's full workspace set.

- **#5042 alias rename** — an alias identifier never participates in query
  planning, so the rename is plan-identical by construction. Confirmed with
  `EXPLAIN` of the shipped `role_grant` form:

  ```
  Limit -> Unique -> Sort (Sort Key: role_grant.feature, role_grant.data_class)
    -> Nested Loop
         -> Index Scan using identity_role_grants_active_idx on ... role_grant
              Index Cond: (tenant_id = 'default' AND role_id = ANY('{owner}')
                           AND effective_at <= now())
              Filter: (expires_at IS NULL OR expires_at > now())
         -> Index Scan using identity_roles_tenant_key_idx on ... role
  ```

  The fixed query drives off `identity_role_grants_active_idx` (indexed seek on
  `tenant_id`/`role_id`/`effective_at`), no sequential or merge-scan fallback.
  Baseline: the pre-fix query never executed at all — it failed at parse time —
  so "after" is strictly an improvement (from 100% parse failure to an
  index-driven plan). Caveat: `identity_role_grants` is empty on the fresh e2e
  stack, so the plan is estimate-shaped; the alias-token invariance argument,
  not row counts, is what proves no plan regression.

## Observability Evidence:

- `#5040` replaces the opaque login-start 503 with `ErrOIDCLoginInvalidRequest`
  (HTTP 400, `error_code` `invalid_request`) for the ambiguous/not-found cases,
  so an operator sees a precise, actionable client-error instead of a generic
  server error, and the resolved `workspace_id` flows into the existing
  login-state row and login logs.
- `#5042` restores the DB-OIDC callback's existing permission-resolution log
  line to success; before the fix it emitted
  `oidc session permission grant resolution failed; login denied ... SQLSTATE
  42601` on every DB-backed group-grant login. No new spans/metrics are added.

## Why the change is safe

- Alias rename is output-preserving: identical result set, identical plan,
  proven by a hermetic static guard (RED against the pre-fix
  `identity_role_grants grant`) plus a DSN-gated real-Postgres `PREPARE` proof.
- Workspace resolution fails closed and is scoped to `WHERE w.tenant_id = $1`
  (no cross-tenant leak); the explicit-`workspace_id` passthrough remains fenced
  by the composite `(tenant_id, workspace_id)` FK on `identity_oidc_login_states`
  and workspace-scoped RBAC.
- End-to-end proof: the #4971 auth E2E passes 16/16 on a fresh zero-identity
  stack, including `item4_member_sso_login_completes` (member OIDC redirect →
  mock IdP → callback → dashboard) and the admin-route/admin-API 403 gating.
