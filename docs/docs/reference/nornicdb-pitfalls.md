# NornicDB Behavior and Pitfalls Reference

This page is the operational-behavior companion to
[NornicDB Tuning](nornicdb-tuning.md). Where the tuning page documents knobs
you can turn at runtime, this page documents NornicDB behaviors that have
caught Eshu off guard during integration testing, dogfooding, and PR work — so
the next person debugging a NornicDB-shaped problem starts ahead of the curve
instead of rediscovering the same failure mode.

## How to use this page

1. Before patching NornicDB or routing around what looks like a NornicDB bug,
   read the matching section here.
2. **Validate the behavior against the latest upstream NornicDB.** Source of
   truth in order:
   - The Eshu-maintained NornicDB fork at
     `/Users/asanabria/os-repos/NornicDB-New` — this is what the Compose image
     `timothyswt/nornicdb-amd64-cpu:vX.Y.Z` is built from. **Do not** read the
     older sibling at `/Users/asanabria/os-repos/NornicDB`; it does not match
     the running binary.
   - Upstream NornicDB documentation and release notes for the binary tag
     pinned in `docker-compose.yaml` (`NORNICDB_IMAGE`). NornicDB evolves
     quickly — a behavior documented here may have been fixed upstream by the
     time you read this. Confirm against the source.
3. If your reproduction differs from what's described here, capture the new
   shape in a Pitfall section below in the same PR. Every pitfall must include
   reproduction steps, observed shape, and either a confirmed root cause or an
   open question.

## Pitfall: Drop-and-recreate of single-property `UNIQUE` constraints on a
live database corrupts the constraint cache

### Observed shape

On a running NornicDB instance with nodes already in storage:

1. `DROP CONSTRAINT <name>` succeeds.
2. `CREATE CONSTRAINT <name> FOR (n:Label) REQUIRE n.prop IS UNIQUE` succeeds.
3. Any subsequent write that touches one of the pre-existing nodes — including
   `MATCH (n {prop: value}) SET n.other = ...` that does not change the
   constrained property — fails commit with:

   ```text
   Constraint violation (UNIQUE on Label.[prop]):
   Node with prop=<value> already exists (nodeID: <the matched node's id>)
   ```

   That is, the constraint check at commit treats the matched node itself as
   a competing entry. `MATCH (n {prop: value}) RETURN id(n)` still finds the
   node, confirming the row is intact in storage.

### Why this happens (working hypothesis)

`CREATE CONSTRAINT` triggers a value-cache rebuild
(`RefreshUniqueConstraintValuesForEngine`) that scans existing nodes via the
storage engine and re-registers values for the new constraint. When the
storage engine in scope is the user-facing `NamespacedEngine` wrapper, the
scan returns nodes with their namespace prefix already stripped
(`NamespacedEngine.toUserNode`). The constraint cache then holds *unprefixed*
node IDs while transactional writes pass *prefixed* IDs through the validation
path. The cache lookup at commit time compares prefixed-vs-unprefixed and
treats the match as a foreign node, raising a false uniqueness violation.

This is the working hypothesis as of the last update of this page; it has not
been confirmed by reading the version of NornicDB pinned at the time you are
debugging. **Verify against `NornicDB-New` source and the upstream changelog
before relying on this explanation.**

### Implications for Eshu work

- **Do not use `DROP CONSTRAINT` / `CREATE CONSTRAINT` cycles as a debug
  experiment on a live Compose stack.** Tear down the stack with
  `docker compose down -v` and start fresh instead — every Compose project
  must already be uniquely named (see [Local Testing](local-testing.md)) so
  the teardown is cheap.
- **Do not patch Eshu's schema bootstrap to re-run `CREATE CONSTRAINT` after
  writes** as a way to refresh anything. The schema DDL must run exactly once
  on an empty database (`db-migrate` / `bootstrap-data-plane`) and never
  again.
- **If you observe false UNIQUE violations on read/update of pre-existing
  nodes**, suspect this pitfall before suspecting a logic bug in the writer.

### Validation guidance

If you need to confirm the behavior against the current NornicDB binary:

1. Stand up a dedicated Compose stack with a uniquely scoped project name
   (e.g. `eshu-nornicdb-pitfall-repro-$$`).
2. Run `bootstrap-data-plane` to create the schema, then write a single test
   node touching a label with a uid-style UNIQUE constraint.
3. Drop and recreate that constraint via the Bolt HTTP endpoint:

   ```bash
   curl -sS -u neo4j:change-me -H 'Content-Type: application/json' \
     -X POST "http://localhost:${NEO4J_HTTP_PORT}/db/nornic/tx/commit" \
     -d '{"statements":[{"statement":"DROP CONSTRAINT <name>"}]}'
   curl -sS -u neo4j:change-me -H 'Content-Type: application/json' \
     -X POST "http://localhost:${NEO4J_HTTP_PORT}/db/nornic/tx/commit" \
     -d '{"statements":[{"statement":"CREATE CONSTRAINT <name> FOR (n:Label) REQUIRE n.prop IS UNIQUE"}]}'
   ```

4. Reissue `MATCH (n:Label {prop: <value>}) SET n.marker = 'test'`. If commit
   fails with the shape above, the pitfall is present in the pinned binary.

Tear the stack down after the experiment. Do not leave the corrupted state
running for other workflows.

## Pitfall: `MERGE` re-projection of an existing node fails the UNIQUE
constraint when a single-property uid constraint is active

### Observed shape

In the Eshu Tier-2 v2.5 tfstate drift verifier (issue #209), Pass 2's
canonical projector reissues `MERGE (r:TerraformResource {uid: row.uid}) SET
...` against a uid that was created by Pass 1. The MERGE does not match the
existing node; it attempts a fresh `CREATE`; the storage-level constraint
check then fails with the same nodeID-points-at-itself shape described in the
drop-and-recreate pitfall above.

`File.path`-keyed MERGE re-projection in the same stack works correctly. The
asymmetry between `File` (works) and `TerraformResource` (fails) is currently
unexplained at the NornicDB level — the same Cypher pattern and constraint
shape against either label.

### Status

**Not reproduced in NornicDB isolation.** Direct Go-test reproductions using
`MemoryEngine + NamespacedEngine` and `BadgerEngine + NamespacedEngine` with
the identical Cypher pattern (UNWIND + MERGE on the uid-constrained label,
then second MERGE on the same uid) commit cleanly. The bug therefore needs
something specific to the live Compose path — candidates being investigated:

- Bolt protocol layer (test bypasses Bolt; production goes through it).
- `AsyncEngine` + `WALEngine` cache-flush timing in combination with new Bolt
  sessions.
- Cross-session cache interactions between `resolution-engine` (Pass 1
  writer) and `bootstrap-index` (Pass 2 writer) as separate Bolt clients.

Track investigation in PR #215 and any issue that supersedes it. Update this
section when the trigger is isolated.

### Implications for Eshu work

- Re-projection of nodes by uid through the canonical writer must not be
  assumed idempotent against the pinned NornicDB binary until this pitfall is
  closed. Plan re-projection-heavy code paths accordingly: either gate the
  write on a prior `MATCH` to detect existing-and-unchanged uids, or restrict
  re-projection to fresh stacks.
- Adding new `uidConstraintLabels` entries in
  `go/internal/graph/schema.go` should be done with awareness of this pitfall:
  any label whose canonical writer re-emits the same uid across Eshu
  generations is potentially exposed.

## When to patch NornicDB itself

Per the **NornicDB Maintainer Patch Bar** in `CLAUDE.md` (and the mirror in
`AGENTS.md`) at the repository root, patches against the NornicDB fork are
acceptable only when the change is evidence-backed:

- a correctness fix for NornicDB itself,
- a measured NornicDB performance win that generalizes beyond one Eshu
  symptom, or
- a measured Eshu runtime win proven by focused and corpus-level evidence.

Before drafting a patch:

1. **Write a failing test in `NornicDB-New` first** per the NornicDB
   `AGENTS.md` mandatory bug-fix workflow. A patch without a failing test is
   not a patch — it is a guess.
2. If the bug does not reproduce in NornicDB isolation, the root cause is not
   in NornicDB. Look at the Eshu-side trigger and patch there.
3. If you do patch NornicDB-New, build the binary into a uniquely tagged
   image (e.g., `timothyswt/nornicdb-amd64-cpu:eshu-<issue>-<sha>`) and pin
   it via the `NORNICDB_IMAGE` environment variable in the relevant Compose
   overlay only. Never overwrite the shared production tag — concurrent
   Compose stacks on the same host run the same image.
