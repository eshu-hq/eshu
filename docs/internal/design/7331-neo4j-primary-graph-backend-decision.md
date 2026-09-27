# Graph Backend Decision: Neo4j Is the Supported Graph Backend; NornicDB Is Secondary (#7331)

Status: Accepted (owner decision, 2026-09-27).

Supersedes the backend statement in `AGENTS.md` ("NornicDB is the default
canonical graph backend. Neo4j is compatibility only…"). Related: #7014 and
PR #7042 (NornicDB repin), #6783 (statement-level execution coverage), #7098
(read-latency budget), #6787, #6916, #7144, #6162.

## 1. Context

Eshu has carried two graph backends behind one Cypher/Bolt contract, with
NornicDB named as the default. By 2026-09-27 the deployed and measured reality
had moved to Neo4j:

- The chart already defaults to Neo4j (`deploy/helm/eshu/values.yaml`,
  `ESHU_GRAPH_BACKEND: neo4j`). `docker-compose.yaml` still defaults its
  services to `nornicdb`. The local CLI path does too: `eshu graph nornicdb`
  installs a managed NornicDB binary (`go/internal/cli/graphinstall`), and
  `local_full_stack` runs on it. There is no managed Neo4j equivalent.
- The shared QA environment cut over to Neo4j Community (2026.08.1, Cypher 25)
  on 2026-09-24/25 and runs NornicDB at zero replicas. Every read-latency fix
  under #7098 has been profiled and proven on Neo4j.
- The pinned NornicDB image (`fix-500-e022384c`) is stale. The repin PR #7042
  has been idle since 2026-09-25 with a required check red. As of
  2026-09-27 its target (`f2163176`) is 266 commits behind upstream main.
- Upstream NornicDB is mid-rewrite (its tracking issue #521). Since 2026-09-19
  about 150 Neo4j-conformance defects were filed there. Many are fixed, but a
  large share were closed as not planned. Those are permanent divergences
  from Neo4j semantics, and several match shapes Eshu uses, for example
  shortestPath between endpoints bound by an earlier `MATCH` (upstream
  #721). Concurrency defects are still being found and fixed: MVCC pruning
  that conflicts with concurrent writes (#732) is open, and a concurrent
  `UNIQUE` insert that committed a duplicate (#700) was fixed upstream on
  2026-09-27, after Eshu's current pin.
- Carrying both backends has a standing cost. Eleven CI workflows exercise
  NornicDB. There is a backend-divergence allowlist, differential gates, and
  NornicDB-specific query variants and pitfall docs. About 680 non-test files
  under `go/` mention NornicDB.

## 2. Decision

1. **Neo4j is the supported graph backend.** It is already the chart default
   for deployments. It becomes the default for local development and CI as the
   phase 2 items in section 6 land; until then `docker-compose.yaml` and the
   local CLI still start NornicDB. Pin it by image digest, and pin the Cypher
   language version it runs (Cypher 25 today).
2. **Correctness, performance budgets and gates are defined against Neo4j.**
   A query is correct when it returns the right rows on the pinned Neo4j. It
   meets its budget when it does so on Neo4j. New query and writer code needs
   no NornicDB-specific variant.
3. **NornicDB is secondary.** It is not supported for deployment, and it stops
   being a merge gate once phase 2 (section 6) moves its CI legs out of the
   required set. Until that ruleset change lands, the required NornicDB checks
   still block merges. `ESHU_GRAPH_BACKEND=nornicdb` keeps parsing, and the
   NornicDB code keeps compiling. Re-qualification is a separate, deliberate
   effort (section 7), taken on when there is time.
4. **Cypher compliance is enforced on Neo4j.** Every inventoried production
   statement is checked against the pinned Neo4j in CI (section 5).

## 3. Rejected alternatives

- **Keep dual-backend parity as a merge gate.** Rejected: it gates every
  change on a backend that is not deployed, and whose upstream semantics are
  changing daily. It also slows the latency work that matters most.
- **Remove NornicDB code now.** Rejected for now: the removal touches hundreds
  of files. It would collide with the active query and reducer work, and
  deleting code is irreversible while re-qualification is still possible.
  Removal is a later decision (section 6, phase 4).
- **Repin NornicDB to upstream main first, then decide.** Rejected: upstream is
  mid-rewrite with open concurrency defects. Making the supported backend wait
  on a moving target inverts the priority order.

## 4. Rationale

The priority order is accuracy, then performance, then reliability.

- **Accuracy.** Neo4j is the reference implementation of the Cypher contract
  Eshu targets. Divergences that NornicDB has closed as not planned cannot be
  fixed upstream; on a dual-backend path, each one forces a query rewrite or
  an allowlist entry.
- **Performance.** Budgets are measured on Neo4j in the deployed environment.
  Backend-specific query variants split the optimization effort, and they add
  code paths that production never runs.
- **Reliability.** Upstream NornicDB concurrency defects touch exactly the
  operations Eshu depends on: concurrent canonical `MERGE` under uniqueness
  constraints (#700, fixed upstream after Eshu's pin) and bootstrap-scale
  writes (#732, open). Any NornicDB pin would have to be re-proven against
  that class of defect first.

## 5. Cypher compliance on Neo4j

PR #6996 (under #6783, which stays open) added a statement inventory
(`go/internal/queryplan/testdata/statement-builders.yaml`) and an opt-in
`golden-corpus-gate -phase=statement-coverage` that records which statements
execute. This decision extends that work into a compliance gate on Neo4j:

- Every inventoried statement, and each rendered variant of the dynamic
  builders, is run through `EXPLAIN` on the pinned Neo4j. Any error fails the
  gate.
- Neo4j notifications are treated as findings: deprecations, cartesian
  products, unbounded variable-length patterns, unknown labels, relationship
  types or properties, and eager plans. Each finding fails the gate. The fix
  is to change the statement. A notification class that is acceptable by
  design, such as an eager plan in a write that needs one, is handled by a
  tested classification rule in the checker, not by a per-statement
  allowlist.
- The Cypher language version is pinned, so a Neo4j upgrade surfaces
  deprecations as work items rather than as surprises.
- The phase will run by default on Neo4j and is intended to become a required
  check. It has not landed yet: it is follow-up work under #6783, and it rolls
  out report-only first.

The openCypher TCK and the Neo4j driver testkit are not adopted for this.
The TCK tests a database engine's semantics, not whether an application's
queries are well formed. Testkit tests Bolt driver implementations. The TCK
remains the right entry test for NornicDB re-qualification (section 7).

## 6. Consequences and phases

1. **This change.** Record the decision and update the backend statement in
   `AGENTS.md`.
2. **Defaults and gates.**
   - Switch the `docker-compose.yaml` defaults to Neo4j.
   - Decide how the local CLI path (`local_full_stack`) gets a Neo4j backend,
     for example a managed container or a documented external instance. This
     needs its own small design, because the CLI currently only installs a
     NornicDB binary.
   - Update the docs that describe NornicDB as the default after the defaults
     switch, not before, so they never describe behavior the code lacks. They
     are `docs/public/why-eshu.md`, `docs/public/concepts/how-it-works.md`,
     `docs/public/reference/cli-system.md`,
     `docs/public/run-locally/local-binaries.md` and
     `docs/public/roadmap.md`, plus `skill-fragments/capability-profiles.md`
     and its generated `expected/` outputs, and the agent-facing
     `go/internal/storage/cypher/AGENTS.md` (the "NornicDB default" notes).
     Dated design and evidence docs that describe NornicDB as the default,
     such as designs #430, #431 and #1314, stay unchanged as history.
   - Switch the CI jobs that run on NornicDB only to Neo4j. Today they are the
     Ifá determinism, dead-letter and fault-injection matrices
     (`scripts/verify-ifa-*.sh`), the replay tier
     (`scripts/verify-replay-tier.sh`) and the read-API latency gate
     (`read-api-latency-gate.yml`). Until this lands, the supported backend
     has no blocking concurrency or latency proof.
   - Move the NornicDB CI legs out of the required set, as nightly or
     advisory jobs. Ruleset changes are an owner action.
3. **Freeze NornicDB-specific growth.**
   - No new NornicDB-only query variants.
   - No new backend-divergence allowlist entries.
   - NornicDB pitfall docs are kept but marked secondary.
4. **Later (owner decision).** Either re-qualify NornicDB (section 7), or
   remove its code paths under a tracked issue with dead-code proof.

Issue dispositions:

- **Secondary, no active work:** #7014 and PR #7042, #6787, #6916, #7144 and
  the NornicDB parts of #6162.
- **Compliance-gate work:** a follow-up under #6783.

What does not change: no code is deleted by this decision, the backend enum
and parsing are unchanged, and existing NornicDB evidence stays in place as
history.

## 7. Re-qualifying NornicDB

NornicDB returns to supported status only when a pinned upstream commit meets
all of the following, measured against the same Neo4j pin:

- It passes the openCypher TCK features covering every clause and expression
  family Eshu's statement inventory uses.
- It has no open upstream defect in the concurrency or row-loss classes that
  matches an Eshu statement.
- The golden-corpus gate and the backend-divergence differential are green,
  with no new allowlist entries.
- Paired, interleaved A/B runs against the same Neo4j pin on the same host
  (at least three runs each, alternating, with load recorded) stay within the
  bar on two measurements:
  - **Writes:** golden-corpus total wall time, and projector and reducer
    phase timings, each within 20% of Neo4j.
  - **Reads:** p95 latency per API route and MCP tool over the #7098 sweep
    argument sets, each within 20% of Neo4j and under the 1 s read budget.

  The 20% figure is the initial bar and needs the owner's confirmation before
  any re-qualification run.
