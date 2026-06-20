# Ask Eshu Tier 2 — Security-Review Design Package

Status: **PROPOSED.**

Refs #3302. Depends on #3250 (Ask Eshu API design), #3261 (sandbox package),
#1755 (Semantic Extraction Security Gates), #1900 (Hosted Governance Policy
Model), #1902 (Tenant and Workspace Isolation). Builds on adversarial-review
findings closed in #3291.

**This document does NOT enable Tier-2.** It is the prerequisite design package
for the human security review that must precede any production enablement. The
`Guard` remains `DEFAULT-OFF` (`enabled=false`). No change in this PR or this
document enables or partially enables the sandbox.

---

## Context

Ask Eshu's Tier-2 path allows the loop's reasoning model to author ad-hoc
Cypher (NornicDB) or SQL (Postgres) queries when no canonical Tier-1 route
covers the caller's question. These are **LLM-authored queries** — not queries
from a curated set, not parameterized templates — running against production
data stores. The trust boundary is therefore adversarial: the model may have
been manipulated by a prompt, by corpus poisoning, or by an attacker who
controls question text.

The sandbox (`go/internal/ask/sandbox`) already enforces three ordered layers:

1. **Normalizer** — adversary-resistant left-to-right byte scanner that masks
   comments, string literals, and dollar-quoted strings so downstream keyword
   scans cannot be bypassed via hidden text; rejects hostile control bytes.
2. **Validate / denylist** — read-only keyword enforcement for both Cypher and
   SQL (write/DDL/DML/transaction-control/side-effecting functions); exactly one
   statement; RETURN required in Cypher; SELECT or WITH must be the first token
   in SQL; double-quoted identifier check for SQL denylist bypass.
3. **Postgres read-only transaction** — defense-in-depth at the database level;
   no write can commit even if layers 1–2 were bypassed.
4. **Cost gate** (added in this PR) — pre-execution EXPLAIN (FORMAT JSON)
   check; rejects plans whose total cost or estimated row count exceeds
   `Caps.MaxPlanCost` / `Caps.MaxEstimatedRows`; optional forbidden-operator
   check (e.g. Seq Scan) via `CostGateConfig.ForbiddenPlanOperators`.

This document covers: the threat model, what is already enforced, what is not
yet solved, three concrete scope-injection options, and the explicit enable
criteria mapped to #1755 / #1900 / #1902.

---

## Threat Model

### Actors and surfaces

| Actor | Control | Goal |
|---|---|---|
| Malicious question author | Query text sent to Ask Eshu | Exfiltrate cross-tenant data, cause DoS via expensive queries, mutate state via write side-effects |
| Compromised model output | Adversarially crafted Cypher/SQL in tool-call payload | Bypass read-only enforcement, cross-tenant reads, schema discovery, resource exhaustion |
| Corpus poisoning | Training-time or retrieval-time injection into model context | Cause model to emit structurally valid but policy-violating queries |

### Attack categories

1. **Write / side-effect bypass**: a query that passes read-only validation but
   executes a write or side-effecting operation (e.g. `SELECT pg_advisory_lock`,
   `SELECT nextval`, `SELECT INTO`).
2. **Cross-tenant read**: a query that returns rows belonging to a different
   tenant than the authenticated caller. This is the primary unsolved problem
   (see below).
3. **Schema discovery**: queries that reveal table names, column names, sequences,
   or other schema metadata across all tenants (e.g. `SELECT * FROM
   information_schema.tables`).
4. **Resource exhaustion (DoS)**: a query that consumes excessive CPU, memory,
   or I/O (full-table sequential scans, Cartesian joins, unbounded aggregations).
5. **Information leakage via denied reason**: error messages that echo the query
   body, reveal schema names, or expose internal state.

### Out of scope (handled by surrounding layers, not the sandbox)

- Authentication and API-key validation — handled by the Eshu auth layer before
  the engine runs.
- Prompt injection that causes the model to produce non-SQL/Cypher output —
  handled by the tool-calling format validation upstream of the sandbox.
- Provider-response retention — handled by `retention_posture: metadata_only`.
- Narration-layer fabrication — handled by the #2462 traceability invariant.

---

## What Is Already Enforced

### Normalizer (Layer 1)

The normalizer (`normalize.go`) defends against **keyword smuggling via hidden
text**:

- Masks `--`, `//`, `/* */` block comments and all string literals (`'…'`,
  `$tag$…$tag$`, `"…"`, `` `…` ``) with spaces before any keyword scan.
- Rejects bytes < 0x20 (except TAB/LF/CR) and DEL (0x7F) that can split
  keyword tokens (e.g. `D\x00ELETE` → tokens `D` and `ELETE`, evading
  `DELETE`).
- Statement counting on `;` outside string/comment context prevents stacked
  statements hidden inside dollar-quote blocks or string literals.

**Adversarial findings closed in #3291:**
- Dollar-quote stacked-statement bypass: `$$; DELETE FROM …$$` was parsed as a
  dollar-quoted literal, hiding the stacked `DELETE`. Fixed by rejecting
  digit-leading `$N$` sequences as positional parameters (not quote openers) and
  by requiring the tag to start with a letter or underscore.
- CR comment-terminator bypass: `--\r FROM pg_sleep(10)` exposed the SQL suffix
  as code because CR was not treated as a line-comment terminator. Fixed by
  treating CR identically to LF in the line-comment state machine.
- Quoted function bypass: `SELECT "pg_sleep"(10)` hid `pg_sleep` inside a
  double-quoted identifier, which the normalizer masked before the denylist scan.
  Fixed by `doubleQuotedIdentifiers()` post-scan in `validateSQL`.
- Sequence/advisory-lock bypass: `SELECT nextval('seq')` and `SELECT
  pg_advisory_lock(1)` are write side-effects callable from a read-shaped query.
  Fixed by adding `NEXTVAL`, `SETVAL`, `PG_ADVISORY_LOCK` (and variants) to the
  SQL denylist.
- `SELECT * INTO newtable FROM …` creates a table inside an otherwise
  SELECT-shaped query. Fixed by adding `INTO` to the SQL denylist.

### Validate / denylist (Layer 2)

- SQL: first token must be `SELECT` or `WITH`; 36-keyword write/DDL/dangerous-
  function denylist; double-quoted identifier post-scan; whole-word matching so
  `update_time` or `created_at` are not false-positives.
- Cypher: 10-keyword write/DDL denylist (CREATE, MERGE, DELETE, SET, REMOVE,
  DROP, FOREACH, CALL, LOAD, DETACH); RETURN required; whole-word matching so
  `:CALLS` does not match `CALL`.
- Both dialects: exactly one statement; `MaxQueryLen` cap; unsupported dialect →
  deny; bounded deny reasons that never echo query body or reveal secrets.

### Postgres read-only transaction (Layer 3)

`sql.TxOptions{ReadOnly: true}` is enforced at the Postgres protocol level.
Even if Layers 1–2 were completely bypassed, the database server rejects any
write statement with an error. This is defense-in-depth, not a primary control.

### Cost gate (Layer 3.5) — added in this PR

`CostGateExecutor` wraps the inner executor and runs `EXPLAIN (FORMAT JSON)`
before the real query executes:

- Rejects plans whose planner total-cost estimate exceeds `Caps.MaxPlanCost`
  (default: 1000.0 cost units).
- Rejects plans whose planner row estimate exceeds `Caps.MaxEstimatedRows`
  (default: 100,000 rows).
- Optionally rejects plans containing forbidden node types (e.g. `Seq Scan`)
  via `CostGateConfig.ForbiddenPlanOperators`.
- Fail-closed: if the EXPLAIN call itself fails, the query is rejected rather
  than allowed.
- Active only when the Guard is enabled (still `DEFAULT-OFF`); zero-cost bypass
  when all limits are zero and no forbidden operators are configured.

The cost gate vocabulary (`Total Cost`, `Plan Rows`, operator names) mirrors
`PlanExpectation` in `go/internal/queryplan`, which is the authoritative gate
for hot-path queries. The sandbox cost gate is the runtime enforcement
counterpart for LLM-authored ad-hoc queries.

---

## The Unsolved Hard Problem: Tenant Scope-Predicate Injection

**This is the primary blocker for Tier-2 enablement.**

The sandbox enforces read-only safety. It does **not** restrict which tenant's
rows a read-only query returns. A valid, policy-compliant `SELECT` query can
still return rows from any tenant in the database.

The existing Tier-1 path solves this via the scoped-token / CTE predicate
pattern (`go/internal/query`, scoped route enablement): every canonical route
has a tenant predicate injected by the route handler, not authored by the
caller. Tier-2 queries are not routed through canonical routes — they are
arbitrary model-authored SQL/Cypher — so the same pattern cannot be applied
mechanically.

The question is: **how do you enforce that an arbitrary LLM-authored SQL query
only returns rows belonging to the authenticated tenant?**

The three concrete options are:

### Option A — Restrict Tier-2 to a curated set of tenant-scoped views or parameterized templates

**Description**: The model is not permitted to author arbitrary SQL. Instead it
selects from a bounded, pre-approved set of tenant-scoped views (e.g.
`tenant_repos`, `tenant_facts`) or calls parameterized query templates
(`get_repo_facts($tenant_id, $repo_id)`). The sandbox validator is extended to
allow only `SELECT … FROM` against this allowlist of view names; any reference
to a base table is rejected.

**Enforcement**: The SQL validator adds an allowlist-only phase after the
denylist check. The views are created with `tenant_id = $1` predicates hard-
coded into their definition at the database level (so a query against the view
cannot select across tenants regardless of the SQL the model authors). The
`SQLExplainer` / `Executor` receives `tenant_id` as a session-level variable or
connection parameter and the views reference it.

**Tradeoffs**:

- Strongest tenant isolation: tenant predicate is enforced at the view
  definition level, not by trusting model output.
- Severely limits expressiveness: the model can only query surfaces the operator
  has pre-built views for. The long-tail value proposition of Tier-2 (ad-hoc
  joins across arbitrary relations) is substantially reduced.
- Operational burden: the view set must be maintained and versioned alongside
  the schema; new query shapes require new views.
- The allowlist check in the validator must handle schema-qualified names and
  aliases correctly to avoid bypass (`SELECT * FROM public.base_table AS
  tenant_repos`).

**Recommendation suitability**: safest for an initial security-reviewed
enablement. The expressiveness loss can be recovered incrementally by expanding
the view set.

### Option B — Execute under a Postgres role with Row-Level Security (RLS) enforced per tenant

**Description**: The Postgres executor opens its read-only transaction under a
per-tenant role (or sets `app.tenant_id` as a session variable) and all
relevant tables have RLS policies that filter rows by `tenant_id =
current_setting('app.tenant_id')`. The model's query runs unchanged; Postgres
enforces the row-level filter transparently.

**Enforcement**: Every fact, repo, and projection table that the sandbox may
query has an RLS policy. The connection pool manages a `SET LOCAL
app.tenant_id = $1` inside the read-only transaction before the query executes.
The cost gate's EXPLAIN also runs with the tenant variable set so cost estimates
reflect only in-scope rows.

**Tradeoffs**:

- Allows arbitrary SQL within the tenant's view — the closest to the original
  Tier-2 design goal.
- Requires RLS policies on every table the sandbox can reach. Missing an RLS
  policy on a new table is a silent data-leak regression. Requires a disciplined
  schema-governance process (a new table must have its RLS policy reviewed and
  applied before it is queryable from Tier-2).
- RLS has performance implications: the planner may not leverage index scans as
  efficiently when the tenant filter is applied via a policy rather than an
  explicit `WHERE` clause. The cost gate must account for this.
- `search_path` injection and `SET` of session variables must be blocked in the
  SQL denylist (already partially covered: `SET` is in the denylist; confirm
  that `SET LOCAL` inside a transaction is also blocked by the read-only-tx
  defense-in-depth before this option is adopted).
- Requires audit that no RLS-bypass superuser functions are accessible.

**Recommendation suitability**: the most expressive option and the most robust
at the database level once RLS policies are comprehensive. High operational
rigor required; suitable as a v2 once the RLS policy set is proven complete.

### Option C — Wrap model output in a scoped CTE the model must `SELECT FROM`

**Description**: The sandbox injects a mandatory `WITH _tenant AS (SELECT …
WHERE tenant_id = $1)` CTE at the top of every model-authored SQL query and
rewrites the query to require that all base-table references pass through
`_tenant`. The model is instructed that it must `SELECT FROM _tenant` rather
than from base tables directly.

**Enforcement**: After validation, the sandbox injects a deterministic CTE and
wraps the model's query as a subquery. The model cannot override the outer
`tenant_id` predicate without authoring a second CTE or a lateral join that
bypasses it, both of which can be detected and rejected by a subsequent
structural check.

**Tradeoffs**:

- Moderate expressiveness: more flexible than Option A (the model can join
  within `_tenant`) but more constrained than Option B (the model cannot
  reference tables outside what `_tenant` exposes).
- CTE injection is complex: query rewriting is fragile if the model's SQL uses
  `WITH` clauses of its own (CTE name collision), or if the structural check for
  bypass paths is incomplete.
- A sufficiently creative model can author SQL that references the `_tenant`
  CTE nominally while also joining to a lateral or a correlated subquery against
  a base table. The structural bypass-check is an adversarial arms race.
- Simpler to implement than full RLS but substantially less robust than either
  Option A or Option B.

**Recommendation suitability**: not recommended as a primary control due to the
structural-bypass risk. Could be used as an additional defense-in-depth layer on
top of Option A or Option B but should not be the sole scope-injection mechanism.

### Recommendation

**Option A (tenant-scoped views with validator allowlist) for the initial
security-reviewed enablement.** It is the only option where tenant scope is
enforced by the database schema definition (view DDL + read-only-tx), not by
trusting model-authored query structure. The expressiveness loss is real but
bounded, and the view set can be expanded incrementally under the same security
review process.

**Option B (RLS)** should be targeted as a v2 path once:
- RLS policies are applied and proven complete across all relevant tables,
- the policy governance process (new table → mandatory RLS review) is
  operational, and
- the cost gate is verified to produce accurate estimates under RLS.

**Option C** should not be used as a primary control.

---

## Explicit Enable Criteria

The Guard (`enabled=true`) MUST NOT be set in any production or staging path
until ALL of the following criteria are satisfied and the human security review
is closed:

### Criteria mapped to #1755 (Semantic Extraction Security Gates)

1. **CR-1** The chosen scope-injection option (Recommendation: Option A) is
   implemented, reviewed, and proven correct against a set of adversarial
   cross-tenant queries that cover all bypass paths described in Option A's
   tradeoffs section.
2. **CR-2** A regression test suite covers cross-tenant query attempts,
   schema-discovery queries against `information_schema` and `pg_catalog`, and
   the full bypass surface (aliases, schema-qualified names, lateral joins,
   correlated subqueries) for the chosen scope-injection approach.
3. **CR-3** The SQL denylist and normalizer have been adversarially reviewed
   against the chosen scope-injection approach. Any interaction between the
   injection mechanism and the existing denylist (e.g. `SET` session variables
   in Option B) must be proven either covered or impossible.

### Criteria mapped to #1900 (Hosted Governance Policy Model)

4. **CR-4** Per-tenant Tier-2 enable/disable policy is wired: a tenant must
   explicitly opt in to Tier-2 execution; the default for all tenants is
   Tier-2 disabled even when the Guard is globally enabled.
5. **CR-5** Per-request and per-tenant rate limits on Tier-2 queries are
   implemented and enforced before the Guard is called: uncontrolled Tier-2
   query rates are a DoS vector regardless of individual query cost.
6. **CR-6** Audit metadata is recorded for every Tier-2 query attempt (allowed
   and denied), every cost gate rejection (with plan cost and row estimate), and
   every scope-enforcement decision. Raw query text, prompt content, and provider
   responses must NOT be in the audit record.

### Criteria mapped to #1902 (Tenant and Workspace Isolation)

7. **CR-7** The scope-injection mechanism is proven to be correctly enforced
   before retrieval — not as a post-hoc filter on returned results.
8. **CR-8** Cross-tenant data access is proven impossible by construction at the
   chosen scope-enforcement layer (database view DDL for Option A; RLS policy
   for Option B), not by trusting the model's query output.
9. **CR-9** Workspace-level isolation (not just tenant-level) is validated for
   all query paths if the data model distinguishes workspaces within a tenant.

### Cost gate criteria (this PR)

10. **CR-10** `Caps.MaxPlanCost` and `Caps.MaxEstimatedRows` are calibrated
    against the production Postgres cluster's cost scale for the largest
    tenant's data volume before enablement. The defaults (1000.0 cost units;
    100,000 estimated rows) are conservative placeholders derived from small
    test data; they must be validated against real query plans.
11. **CR-11** The `CostGateConfig.ForbiddenPlanOperators` list is reviewed and
    set based on actual query plan analysis for the tables reachable from Tier-2.
    At minimum, `Seq Scan` on large fact tables should be forbidden.

### General criteria

12. **CR-12** The human security review (this document's audience) is complete
    and signed off by the security reviewer. No code change in this PR or any
    child PR constitutes sign-off; sign-off requires explicit human review.
13. **CR-13** A runbook documenting how to disable Tier-2 at runtime (flip the
    Guard to `enabled=false` without a deploy) is available and tested.

---

## What This PR Delivers

1. **Cost gate implementation** (`costgate.go`, `costgate_test.go`): `CostGateExecutor`
   wrapping the inner executor; `SQLExplainer` interface for mock-testable EXPLAIN
   integration; `CostGateConfig` with `ForbiddenPlanOperators`; `PlanSummary`
   observable; `ErrPlanBudgetExceeded` sentinel. Gated behind the existing
   `Guard.enabled=false` default. All 13 new tests pass; existing sandbox tests
   are unchanged.

2. **`Caps` extension** (`sandbox.go`): `MaxPlanCost` and `MaxEstimatedRows`
   fields with conservative defaults (1000.0 and 100,000 respectively).

3. **This design document** (`docs/internal/design/3302-ask-eshu-tier2-enablement.md`):
   threat model, enforcement audit, scope-injection options with tradeoffs and
   recommendation (Option A), explicit enable criteria mapped to #1755/#1900/#1902.

**What this PR does NOT deliver:**
- Tier-2 enablement.
- Scope-predicate injection (Option A/B/C — any of them).
- Per-tenant opt-in policy.
- Audit wiring.
- Human security review sign-off.

---

## Verification

```
cd go && gofmt -l ./internal/ask/sandbox         # empty
cd go && go vet ./internal/ask/sandbox           # clean
cd go && golangci-lint run ./internal/ask/sandbox/...  # 0 issues
cd go && go test ./internal/ask/sandbox -count=1 # PASS (all existing + 13 new)
git diff --check                                 # clean
```
