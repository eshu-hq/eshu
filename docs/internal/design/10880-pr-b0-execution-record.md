# PR-B0 execution record for #10880

Refs #10880. Companion to
[the PR-B evidence record](10880-pr-b-reader-moves-and-scoped-fork.md), which holds
the measured divergence, the reader inventory, and the audit design this record
executes. Written 2026-11-22.

Arbiter ruling: option B for every grant-scoped and existence-filtered read,
canonical pick-then-filter for rank filters, any-support filter-then-pick for
existence filters; PR-B splits into B0..B3 and the legacy read retires in PR-C.
B0 lands the pieces that need no supports table. Commits on
`drive/10880-pr-b`, each test-first:

1. **Advisory evidence declaration.** `supply_chain.advisory_evidence.list`
   now declares `fact_records_collector` and `fact_records_reducer`. The
   lockstep probe failed on the old row first. The catalog regenerated and
   verified through the stub satellite harness, and the "source-only" note
   says impact findings are read only to select evidence.
2. **Multi-support live proof.** The corpus of section 1, plus two derived-class
   facts, is committed as a live test (`TestMultiSupportScopedGrantDivergenceLive`,
   `TestMultiSupportUnscopedRankFilterDivergenceLive`,
   `TestMultiSupportWinnersRecordsEverySupport`), enrolled in the
   live-postgres-readiness plan. It pins today's answers, so a later PR cannot
   change them silently: the legacy and winners answers for each grant shape and
   rank filter, the gate-on stores keeping granted calls on the legacy read, and
   derived-class facts appearing in no answer. One derived fact sits in an
   `image://derived/` scope and one in an ordinary scope, so scope-prefix
   exclusion and classification exclusion are each shown to hold alone; dropping
   the classification turns the proof red. The third grant leg in the legacy
   SQL, `OR finding_class.source_class = 'derived'`, is dead in static mode: the
   derived branch is closed by a literal `AND FALSE`, so no derived-class row
   reaches it. The PR-B3 expectations for rank filters change on purpose.
3. **Layer-2 audit** (`go/internal/query/store/audit`). A recorder, a
   classifier, a source census, and tests that read the real matrix. The PR may
   claim only this: the audit enforces used ⊆ declared for the listed
   capabilities and variants; the rest are waived and may only shrink; graph and
   content reads are not recorded; the census is a package-union floor; and
   #10880 acceptance criterion 1 is met for the listed capabilities only. AC1
   stays open until both waiver lists are zero (issue #11212); that is not a B3
   precondition.
   - First real find: the advisory evidence declaration (reverting it fails
     three audit tests). Second: the readiness block of the impact list and
     explain responses reads `fact_records_collector` and nine tables that no
     capability declared. Readiness probes (repository, CVE, image reference,
     and package anchors, with canned rows for the readiness gate; the CVE and
     package variants stop at target resolution and are named so, the repository
     and image-reference anchors reach the readiness statements) now run under
     both capabilities, and list and explain declare what they prove:
     `collector_instances`, `container_image_identity_supports`, three
     `package_manifest_consumption_*` tables, `package_registry_identity_keys`,
     two `vulnerability_source_state*` tables, and `workflow_runs`, plus
     `fact_records_collector` on list. Contract: a capability declares every store
     whose rows can change any part of the response (rows, filter inputs,
     readiness, freshness); only a join that scopes a read to a generation or
     authorizes it, and adds no content, is exempt.
   - The winners freshness watermark is the winners store (one resweep writes
     both), so it classifies as `read_table:supply_chain_impact_canonical_winners`.
     PostgreSQL system catalogs are schema introspection, not a store.
   - The classifier fails closed. The review of the first version seeded seven
     shapes that produced no violation; each now has a seeded RED/GREEN case:
     comma-joined `FROM` lists, `(X OR TRUE AND FALSE)`, `a AND (NOT FALSE) OR
     (live)`, `--` inside a literal, a CTE named like a store, a quoted
     identifier, and a mixed literal and non-string kind bind. Dead-branch removal
     follows SQL precedence.
   - Probes cover impact list, aggregate and inventory, grouped inventory,
     explain, advisory evidence and catalog, and both alert-reconciliation
     capabilities. Round-2 review found the readiness probes recorded only the
     fallback statement; production runs the protected one. The probes now answer
     the layout probe both ways (`fallback statement` and `protected statement`
     variants under list and explain). The protected statement reads one scanner
     table live: `scanner_image_targets`, in a `NOT EXISTS` inside the live static
     arm of `currentScannerFactPredicate`, which changes which facts count toward
     readiness, so list and explain declare it. The receipt chain (tenants,
     workspaces, role policy, grants, epochs, attempts, results, observations)
     sits in an `EXISTS` subquery whose `WHERE` ends in a literal `AND FALSE`
     ("the final guard remains closed"), so it returns no rows whatever it reads;
     the classifier drops a subquery whose top-level `WHERE` is always false. The
     25 scanner tables left in the supporting list are reached only through that
     dead receipt chain and the closed derived branch, and no probe reaches them
     live. An earlier version of this record said every authority table reaches a
     response only through the closed derived branch; that was wrong for readiness
     and is corrected here.
   - Round-2 classifier fixes, each with a seeded RED/GREEN case: a comma after a
     JOIN item, a parenthesized join tree, a CTE that reads its own name, and
     `fact_kind` compared with `LIKE`, `<>`, `!=`, or `~`. Fact kinds are still
     attributed per statement, not per relation (README states the limit), so
     kind-selecting operators fail closed instead.
   - Round-3 classifier fixes, each with a seeded RED/GREEN case: `trim`,
     `substring`, and `overlay` blank only the `FROM` at their own argument depth,
     so a subquery inside them stays a read (the greedy match used to blank the
     subquery's `FROM`); a compound subquery is read arm by arm, so an empty first
     `UNION` arm no longer hides a live second arm; `fact_kind` as a bare function
     argument, a `NOT IN` that names an unread kind, and `NOT LIKE` fail closed;
     a quoted `"true"` or `"false"` identifier fails closed. The classifier
     recognizes a kind predicate only as `=`, `IN`, and `= ANY($n)` on that
     alias; casts, `COLLATE`, `||`, `position()`, `^@`, ranges, `ANY(ARRAY[..])`,
     `IN (SELECT ..)`, `NOT (=)`, `IS DISTINCT FROM`, a row compare, and a
     `NOT IN` that crosses aliases are not classified. Round 4 adds the kind
     canary (`canary_test.go`): every live `fact_records` alias in every recorded
     probe statement must carry a recognized positive kind predicate of its own,
     searched in the ref's own subquery arm (a reused alias elsewhere, or another
     `UNION` arm's predicate, does not count) and restricting (a depth-zero `AND`
     conjunct of the `WHERE` or `ON` clause, not an `OR` arm, `NOT`, function,
     `CASE`, select-list, `IS`, or cast operand). Round 5 added both rules after
     the reviewer showed an alias reused in another CTE satisfying a kindless
     ref. Three `fact_id` join-backs are pinned (the impact list's `source` and
     `override`, the readiness statement's package-manifest `fact`), each with its
     reason; the 20 hidden forms, the scoping cases, and the eight non-restricting
     forms are its seeded RED set. It enforces the probed capabilities only: the
     155 waived capabilities are not enforced (#11212).
   - Waivers: 155 capabilities have no probe and 18 query packages have no census
     owner. Raising a ceiling constant is a code edit that only review catches.
4. **`image_ref` guard.** The gate-on list and aggregate stores serve an
   `image_ref` call from the legacy read. Grouped inventory has no legacy read, so
   it answers `ErrGroupedInventoryFilterUnavailable`, a wrapper of
   `ErrGroupedInventoryReadModelUnavailable`, which the handler maps to a 503
   whose text names the filters (it no longer reads like a gate-off 503).
5. **Profile proof: supports of one key can differ.** The detection profile
   is a function of status, observed version, and match reason. The first two
   are in the canonical key; the match reason is not, and the package path never
   sets the criteria ids. On the OS path a vendor exact-version list and a tagged
   OSV range both give `affected_exact` for one observed version and release
   line, with reasons `dpkg_exact_affected_version` (precise) and
   `dpkg_affected_range` (comprehensive).
   `TestDetectionProfileIsAPerSupportOutputOfOneCanonicalKey` pins it; no writer
   invariant can hold, so B0 adds none. The arbiter ruling (decision 2) makes
   `profile` an existence filter with any-support semantics, served from supports
   in B2. The committed seed has keys p1 and p2, each with a precise and a
   comprehensive support (p2's winner is the comprehensive one), and
   `TestMultiSupportProfileDivergenceLive` pins that `profile=precise` loses p2 on
   the winners query and keeps it on the legacy read. The gate-on list and
   aggregate stores route any non-empty profile filter to the legacy read, and
   grouped inventory answers the same 503 as `image_ref`. Because `precise` is
   the API default, a default call reads the legacy path with the gate on; only
   `profile=comprehensive` calls with no grant and no `image_ref` read the winners
   table. The gate defaults to off, and the flip (B3) cannot happen before B2, so
   the cost falls only on deployments that turned the gate on early.
6. **Gate text and HTTP reference.** The `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ`
   description no longer says the findings output is byte-identical; the env
   reference regenerated; the inventory HTTP reference gained a section on rank
   and existence filters on multi-scope findings and a corrected "bodies match"
   sentence.

### The derived grant leg is dead

The legacy grant clause ends in `OR finding_class.source_class = 'derived'`.
`supplyChainImpactProtectedSourcePredicateSQL` ANDs a predicate into the same
`WHERE` whose derived arm ends in a literal `AND FALSE`, independent of the
read-mode marker, so no derived-class row survives to meet that grant arm in
static or protected mode, in `listFindingsQueryTemplate`,
`aggregateCanonicalFactsCTETemplate`, or `buildExplainSupplyChainImpactFindingQuery`.
`ListFindingsProtectedQuery` has no production caller. B1 and B2 need not
reproduce the arm; the legacy SQL keeps it, unchanged, because the legacy read is
the rollback. The B1 equality test and the B2 reader tests carry the derived-class
rows of the seed for every grant shape, including a scope grant that does not
cover the derived fact's scope, and assert their absence in both reads. Opening
derived reads (#9468) makes that arm a grant bypass for derived rows; the winners
and supports reads have no equivalent, so #9468 owns the derived read design on
supports.

### Checklists carried forward

- **B2:** profile served from supports and the profile guard removed; `image_ref`
  served from supports and its guard removed; the grouped-inventory 503 for both
  filters removed; scoped list, aggregates, grouped inventory, explain, and
  advisory evidence move to supports.
- **PR-C:** remove the legacy read, the gate, `ListFindingsProtectedQuery`, and
  the dead derived grant leg together.

### For the PR body

Name plainly: with `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` on, grouped inventory
(`service_id`, `workload_id`, `environment`) answers `503` for the default
profile and for `image_ref`. Before B0 it returned a `200` that could under-count.
There is no CLI caller and no in-repo configuration turns the gate on; the 503 is
interim and its removal is the B2 checklist item above.

### What B0 does not do

No supports table, no reader switch, no default flip, no change to the legacy
read. The scope-id grant divergence and the rank-filter divergence stay in the
code exactly as section 1 measured them, now pinned by the live proof.

### NOT_CHECKED in B0

- The live proofs ran against a private `postgres:18` container; no QA or
  production census of multi-support keys exists.
- Golden-corpus API and MCP equality (acceptance criterion 3) and the
  read-latency gate are not part of B0.
- The census is a floor: a SQL literal outside the `KnownRelations` set, or a
  query built without string literals, is invisible to it.
