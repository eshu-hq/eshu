# Ifá workflow commentary

These are the original workflow comments in source order.

```yaml
# P4 (#4397) advisory->blocking flip (design doc
# docs/internal/design/4389-ifa-conformance-platform.md, line 517's RULE): the
# Ifa graph-determinism matrix (Layer 2, scripts/verify-ifa-determinism.sh)
# and its dead-letter failure-path leg (scripts/verify-ifa-dead-letter-
# matrix.sh) become per-PR path-filtered BLOCKING CI gates, mirroring
# golden-corpus-gate.yml. The RULE requires the N-sensitive Tier 2 run (the
# demo-org cassette PLUS the 8-project synth-multiscope cassette) — a gate
# that only ever ran the single-generation demo-org cassette could stay green
# forever without ever exercising a genuine worker-count race. Both matrix
# scripts already drive that Tier 2 combination; this workflow wires them
# into per-PR CI instead of leaving them local-only.
  # Merge-queue entries (#7111): every job runs, publication stays off, and
  # required-gates.yml aggregates the merge_group check rows.
      # NOTHING GATES THIS LIST against specs/ci-gates.v1.yaml. The registry's
      # drift check (scripts/verify-ci-gates-registry.sh) compares trigger
      # strings only for workflows that select jobs with a dorny/paths-filter
      # step; this workflow uses a plain `on.paths` list, so a path deleted here
      # while its registry trigger stays leaves that check at exit 0 -- measured,
      # not assumed. Keep this list in step with the `ifa-determinism`,
      # `ifa-dead-letter-matrix` and `ifa-fault-injection` triggers by hand.
      # `push:` above has no paths filter, so main-push coverage is unaffected.
      # The scripts/lib/ Ifá surface as globs, not the 65 filenames that were
      # here (#6200). This filter is shared by all four jobs below -- there is
      # no per-job `if:` -- so it is the single thing that decides whether ANY
      # Ifá live proof re-runs on a change. It named files one at a time, and
      # the blocking 500-line cap splits this directory more often than any Go
      # package: each split left the original filename in place, so nothing
      # dangled, while the new half started no job at all. Two files were dark
      # that way on main --
      # test-ifa-fault-injection-deployable-unit-kill-isolation-cases.sh and
      # test-ifa-fault-injection-generic-runner-lease-audit-cases.sh.
      #
      # These strings must stay spelled EXACTLY as specs/ci-gates.v1.yaml
      # spells them: the registry-subset-of-workflow lockstep in
      # scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh compares
      # trigger STRINGS without expanding globs, so a registry-only glob
      # leaves both gates selectable-but-never-started (#6164's shape).
      #
      # Several of these also close a hole the sourced-to-triggered drift walk
      # cannot see on its own: the fault dispatcher and the mirrors load their
      # modules through VARIABLE paths, and internal/cigates/scripttrigger.go
      # only resolves a literal "scripts/" inside a source line.
      # The per-family row files, not just the orchestrator. The orchestrator
      # holds no family data at all -- every blocker_kind, wait_key and anchor
      # lives under rows/. Naming only the parent file would leave the exact
      # edit we make most often (correcting one family's row) triggering
      # nothing.
      # The per-family pin files, sourced by variable path from the pins
      # module. Their governing rule is that pins are hand-derived and never
      # generated from the registry; editing one to "just make it match" has
      # to re-run the gate that would object.
      # The ifa package root, replacing 12 filenames and two <family>_* globs
      # (#6200). go/cmd/ifa imports this package from its own production
      # files, so every non-test file in it compiles into the binary both live
      # jobs run; 19 were dark, including the shared Odù machinery (odu.go,
      # expectations.go, coverage.go) and the compiled Odù for code_call and
      # rationale. Same string-exact registry lockstep as the two globs above.
      #
      # Deliberate cost, not an oversight: a paths: glob cannot carve out
      # *_test.go, so this row and the reducer/factschema globs also sweep in
      # 826 test files (27 here, 768 under go/internal/reducer/**, 31 in
      # sdk/go/factschema). A test-only edit therefore arms both live Docker
      # gates even though test files compile into no binary those gates build.
      # The "one package, one binary" rationale above covers production files
      # only; the alternative is re-enumerating filenames, which is the drift
      # this glob exists to end. Accepted: over-triggering costs CI minutes,
      # under-triggering ships a dark gate (#6200).
      # The family fixtures moved to go/internal/ifa/familyodu/ (#6594 P1), which
      # the glob above does not cross into. These literals mirror the registry's
      # stem-pin rows string-exactly (the lockstep compares trigger STRINGS), so a
      # family edit keeps arming this workflow's gates.
      # ifa-dead-letter-matrix's own two registry triggers, kept as literals
      # beside the glob that already covers them: that gate shares this
      # workflow's single paths: filter, and the registry-subset-of-workflow
      # lockstep compares trigger STRINGS without expanding globs. That loop
      # covers ifa-dead-letter-matrix as of #6200 -- before that it iterated
      # only determinism and fault-injection, so nothing asserted this gate's
      # triggers were a subset at all and these literals were unenforced.
      # One entry, not the ~40 filenames that were here (#6200). Both live
      # jobs below build the whole eshu-reducer binary, so any non-test file
      # in this package can change what they prove; a hand-picked subset went
      # dark every time the blocking 500-line cap forced a split, because the
      # original filename kept its entry and nothing dangled. specs/
      # ci-gates.v1.yaml carries the same single entry for both gates, and the
      # registry-subset-of-workflow lockstep in scripts/lib/
      # test-ifa-determinism-registry-lockstep-cases.sh compares trigger
      # STRINGS without expanding globs -- so this line must stay spelled
      # exactly as the registry spells it or the gates become
      # selectable-but-never-started.
      # The factschema root package and the two v1 packages whose facts the
      # driven cassettes carry, replacing six filenames (#6200). The decode
      # machinery every Decode* funnels through (decode_map.go's
      # decodeMapIntoWith/assignField, fields.go's requiredPayloadKeys) and
      # the encode_direct.go helpers that build the synth cassette had split
      # out of decode.go under the line cap and were named by nothing. Same
      # string-exact lockstep as the reducer entry above.
      # The fault-injection mirror reads reopenSucceededReducerWorkQuery and
      # ReopenSucceededReducerSetClause from the replay file (#7823): a refactor
      # there must re-run this gate, as #7807 proved by merging red.
      # The guards moved to go/internal/ifa/materializededges/ (#6053), so the
      # former per-file glob for those guards matched nothing once they moved, so
      # it was dropped here and from all three gate blocks in ci-gates.v1.yaml
      # together -- the registry-subset-of-workflow lockstep compares trigger
      # strings, so removing it from one side alone breaks the mirror.
      #
      # Without the line below both live gates are SELECTED as blocking by the
      # registry and then never started by GitHub, because a PR touching only the
      # moved package matches no path here. The determinism mirror's
      # registry-subset-of-workflow loop is the only check that sees this -- the
      # registry lane and --drift both pass while it is broken.
      # The determinism matrix drives the Tier-2 synth-multiscope cassette
      # generated by go/internal/synth/gcp (GenerateMultiScope). A change there
      # that broke scope disjointness would make the worker matrix go inert, so
      # it MUST retrigger this gate.
      # P6 (#4580) deterministic fault-injection: the in-binary decorator, its
      # build-tagged classification regression test, the reducer fault wiring,
      # and the Docker gate that proves real recovery (zero dead letters, graph
      # identical to the fault-free baseline). A change to any of these must
      # re-run the fault matrix.
      # The fault job installs ripgrep through this before running the gate,
      # and the gate's preconditions shell out to rg.
      # #6162: every Go-building job (determinism-matrix, dead-letter-matrix,
      # fault-injection) pre-warms modules through this shared retry helper
      # before its first build/test step. verify-ci-gates-registry.sh checks
      # every job's `run:` line against its gate's registry triggers, so this
      # row keeps a PR editing only the helper from selecting no gate locally
      # and failing first in CI.
      # #5351 materialized-edge exhaustiveness gate: the SQL relationship family
      # cassette is driven into every determinism cell (with a per-cell
      # `ifa assert-edges`) and every fault-injection cell (with a baseline
      # `ifa assert-edges`), so the materialized_edges:sql_relationships manifest
      # row's proof_gate: ifa-determinism / ifa-fault-injection claims are backed
      # by a live replay of the family. A write-path-only regression to the SQL
      # edge writer or materialization handler is exactly the silent-no-op class
      # the pure seam cannot catch, so it MUST retrigger these live lanes.
      # code_calls (#5991) is driven and set-exactly asserted in every
      # determinism cell and in the fault-free and domain-scoped recovery cells.
      # Any cassette change therefore retriggers both live proof lanes.
      # The expected-edge-set assertion files `ifa assert-edges` compares the
      # live graph against live under this package-local testdata tree (NOT
      # testdata/cassettes/, which the offline cassette validator scans). A
      # change to the expected set changes what the live lanes assert, so it
      # MUST retrigger them.
      # The live lanes compare code_calls against this exact expected set.
      # documentation_edges (#5994) is driven and set-exactly asserted in every
      # determinism cell, pre- and post-delta, and in the fault-free and two
      # domain-scoped recovery cells. Its cassette and its expected-edge set are
      # the same two artifacts the resolver reads, so a change to either changes
      # what the live lanes assert and MUST retrigger them.
      # Glob, not the canonical_ literal: the registry declares this shape, and it
      # is what covers edge/writer/documentation_labels.go -- buildDocumentationRowMap's
      # target_kind switch, whose routing the family guard defers to the live assert.
      # codeowners_ownership_edges (#5992). Its Odù, cassette, expected-edge
      # set, reducer stage, writer, and live/fault helpers are all inputs to
      # what the live lanes assert, so each one must retrigger them.
      # Glob though only canonical_codeowners_edges.go exists today: the
      # documentation family's literal was correct until its writer split into
      # a second file.
      # handles_route/runs_in/invokes_cloud_action trio (#5995/#6000/#5997):
      # one shared live lib (sourced by BOTH gates), one fault-ONLY cells
      # file (verify-ifa-determinism.sh never sources a *_cells.sh file for
      # any family -- only its live-lib companion).
      # submodule_pin_edges (#6002). Its Odù, cassette, expected-edge set,
      # reducer stage, and live/fault helpers are all inputs to what the live
      # lanes assert, so each one must retrigger them. The family's guard
      # (submodule_pin.go) lives in
      # go/internal/ifa/materializededges/, already a trigger above (the
      # #6199 split's directory glob) -- not repeated here. Its reducer-side
      # decode file (go/internal/reducer/factschema_decode_submodule.go) and
      # the SDK-side decoder and fact-kind constant it calls into
      # (sdk/go/factschema/decode_submodule.go, fact_kinds_submodule.go) are
      # covered by the two package globs at the top of this list. The SDK pair
      # needed a dedicated '*submodule*.go' entry until #6200, because neither
      # file sits under submodule/v1/ and only that subtree was covered; the
      # package glob subsumes it. A change that quarantined or mis-decoded
      # every cassette pin at that seam would leave the coverage rows #6002
      # unwaived backed by stale results, with no gate re-running to notice.
      # deployable_unit_edges (#5993) is driven and exact-set asserted in its own
      # standalone determinism cell (after the shared N-loop, since materializing
      # anything requires a bootstrap-index maintenance pass none of the other
      # cells run) and in the family-scoped baseline plus two domain-scoped
      # recovery cells on the fault-injection gate. Its live lib was split
      # three ways under the 500-line cap and each half needed its own row
      # here until the ifa_*_live*.sh glob above replaced all three -- that
      # split is the exact shape #6200 is about, and the next one now needs no
      # row at all.
      # Pre-existing gap surfaced by the registry-derived check below: the fault
      # gate triggers on this in the registry and the selector table, but the
      # workflow never listed it.
      # rationale_edges (#5998) is replayed and full-record asserted by both
      # live lanes. Keep the handler, its directly used helpers, graph writer,
      # replay canonicalizer, fixtures, and shared assertion helper in the
      # workflow union. The fault-cell files still select only the fault
      # registry gate during local path selection.
      # kubernetes_namespace_environment / iam_instance_profile_role (#6228),
      # the first DIRECT-materialization families in these matrices. Mirrors
      # the paths specs/ci-gates.v1.yaml lists for BOTH the ifa-determinism
      # and ifa-fault-injection gates since #6309 landed the fault cells. The
      # registry-subset-of-workflow check fails if the two drift, because a
      # gate selected as blocking from a trigger this filter omits is selected
      # and then never starts.
      #
      # This single shared on.pull_request.paths filter ARMS all four jobs on
      # these paths, since there is no per-job condition.
      # Mirrors specs/ci-gates.v1.yaml's fault-block literals so the
      # registry-subset-of-workflow lockstep holds for the per-family
      # reach-ins (same treatment as the inheritance/shell_exec/
      # repo_dependency cells literals above).
      # handles_route/runs_in/invokes_cloud_action trio (#5995/#6000/#5997):
      # one shared cassette and Odù (symbolRuntimeFamilyOdu() is registered
      # in catalog_seed.go's catalogSeed, so it is live-binary-consumed, not
      # test-only), three SEPARATE expected-edge directories (one per
      # family's own exact-set assertion). symbol_runtime_family_cassette.go
      # is deliberately NOT listed here: LoadSymbolRuntimeFamilyOdu is called
      # only from a Go unit test
      # (materializededges/symbol_runtime_family_odu_test.go), never from
      # any binary the live gates invoke.
      # The trio's PRODUCTION paths -- without these an extractor or MERGE
      # identity edit does not re-run this gate, leaving the unwaived coverage
      # rows green on a stale live proof. Mirrors specs/ci-gates.v1.yaml.
      # Shared committed family fixture paths + existence guards, sourced by
      # BOTH live gates: a change here changes what every cell drives.
      #
      # Mirrors of the registry rows the materialized-edge lockstep pins by
      # literal text. Broader globs above already cover these files, but
      # test-ifa-determinism-registry-lockstep-cases.sh compares the registry
      # against this list with rg --fixed-strings: a registry trigger absent
      # here marks both gates BLOCKING while GitHub never starts them, and the
      # required-gates publisher then waits forever on checks that never
      # arrive. Keep this list and the registry rows in lockstep (#6200).
      #
      # The rest of ifa-dead-letter-matrix's registry triggers, as literals.
      # Globs above already cover these files, so the gate did fire without
      # them -- but it fired by coincidence of those globs, not because
      # anything asserted it. The registry-subset-of-workflow loop in
      # scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh now covers
      # ifa-dead-letter-matrix too, and it compares trigger STRINGS, so these
      # rows are what make that invariant asserted rather than incidental
      # (#6200).
# Least privilege: the gate only checks out and reads the repo.
      # The validators below run `go run ./cmd/ci-gates` (registry lockstep
      # cases), so this job needs the same toolchain and module pre-warm as the
      # matrix jobs; without it a cold runner fetches modules un-retried inside
      # a validator (#6706 review).
    # --keep below skips the script's own compose teardown, and the script
    # otherwise names its project with a PID suffix this workflow cannot
    # guess; pin it so the log-dump and teardown steps address the real stack.
      # Pre-warm modules with retries so a dropped proxy stream fails HERE,
      # named for what it is, instead of surfacing as a determinism-matrix
      # red that never ran a cell (#6075 follow-up; runs 34627429845,
      # 34007134864, 33551099795, 34370706054).
      # No retry-to-green: this platform's flake policy (design doc P4)
      # forbids normalizing a real divergence away by retrying or lowering N.
      # --keep: without it the script's own EXIT trap deletes the work dir
      # (every N's canonical graph dump, rationale-delta dump, and host binary
      # logs) before the failure-artifact upload below can run (#6162).
      # #6162: upload the kept (--keep) work dir's per-N canonical graph
      # dumps, rationale-delta dumps, and host binary logs, not just the
      # backend container log tail above. A mismatch (e.g. a rationale
      # non-convergence, run 33788992677) needs the actual bytes, not just the
      # printed digest, to diagnose.
    # --keep below skips the script's own compose teardown, and the script
    # otherwise names its project with a PID suffix this workflow cannot
    # guess; pin it so the log-dump and teardown steps address the real stack.
      # Pre-warm modules with retries so a dropped proxy stream fails HERE,
      # named for what it is, instead of surfacing as a dead-letter-matrix red
      # that never ran a cell (#6075 follow-up).
      # --keep: without it the script's own EXIT trap deletes the mutated
      # cassette and host binary logs before the failure-artifact upload below
      # can run (#6162).
      # #6162: upload the kept (--keep) work dir's mutated cassette and
      # host binary logs, not just the backend container log tail above.
  # Sharded across four parallel runners. The matrix ran serially until the
  # cell count outgrew the 30-minute ceiling: a measured 21m2s for 18 cells
  # (~1.17 min/cell) left room for roughly seven more cells. The matrix reached
  # 21 before this change landed (~24.6 min at that rate), which is the argument
  # for sharding rather than a footnote to it. The
  # materialized-edge exhaustiveness program adds two per family for eight
  # remaining families. Raising timeout-minutes would have bought time without
  # removing cost; sharding is the only lever that takes the per-cell fixed
  # waits -- the 1-minute reducer lease reclaim and the 30s queue-retry delay,
  # both production constants the gate exists to prove, so neither may be
  # shortened for the gate's convenience -- off the critical path, because
  # parallel runners absorb them concurrently.
  #
  # Shard membership comes from scripts/lib/ifa_fault_shard.sh, which
  # partitions over atomic cell GROUPS (a family-scoped baseline always
  # travels with its own recovery cells) and runs cell_baseline first in
  # EVERY shard -- it is the sole writer of digests[baseline], which every
  # other cell compares against in-shell, so re-running it per shard is
  # deliberate rather than plumbing that digest between runners.
  #
  # What that trade costs, stated plainly: before sharding, ONE process computed
  # digests[baseline] once and every recovery cell without a family-scoped
  # baseline of its own compared against it. There are now four, one per runner,
  # computed independently, and nothing in this workflow compares them to each
  # other -- a fault-free drive that produced a different digest on runner 2
  # than on runner 1 would go unnoticed HERE.
  #
  # That is acceptable because the comparison is not this gate's job. Proving
  # the same fault-free drive is byte-identical across separate processes and
  # worker counts is exactly what the sibling determinism-matrix job above
  # asserts (N=1,2,4 against the demo-org and synth-multiscope cassettes). This
  # job assumes cross-run baseline determinism and proves RECOVERY instead --
  # that each fault, injected against its own freshly-computed baseline, still
  # converges to it. If cross-run baseline determinism itself regressed, the
  # determinism-matrix job is where it would be caught. If that job is ever
  # narrowed, this assumption has to be revisited.
  #
  # The matrix.shard list below must stay in lockstep with
  # IFA_FAULT_SHARD_DEFAULT_N in that library;
  # scripts/test-verify-ifa-fault-injection.sh asserts the matrix cardinality
  # matches, so deleting a shard here reds the mirror rather than silently
  # dropping that shard's cells from every run. Deliberately phrased without
  # reproducing the literal list: an earlier version of this comment spelled
  # it out, and the cardinality pin's first draft matched THIS PROSE instead
  # of the real declaration below -- a pin reading its expected value out of a
  # comment passes no matter what the matrix says. Keep the literal in one
  # place only, the declaration itself.
      # Pre-warm modules with retries so a dropped proxy stream fails HERE,
      # named for what it is, instead of surfacing as a fault- or
      # race-shaped red that never ran anything (#6075 follow-up; runs
      # 34627429845, 34007134864, 33551099795, 34370706054).
      # The gate's own preconditions shell out to rg (the repo forbids grep), and
      # the fault-injection job did not install it -- #6173's first CI run failed
      # both codeowners-carrying shards with "rg: command not found" reported as
      # "declares no IntentWriter", a confident false diagnosis of a file that
      # holds the field. The static-mirror job above already installs it for the
      # same reason.
      # The classification regression test that pins the P6 fault-error
      # fidelity fix is behind the ifafaultinjection build tag, so a normal
      # `go test ./...` never runs it. Run it explicitly here, alongside the
      # tagged decorator and reducer-wiring tests, so a revert of the
      # Retryable()/FailureClass() contract fails CI even before the Docker leg.
      # No retry-to-green: a non-zero dead_letter count or a canonical graph
      # digest that diverges from the fault-free baseline is a real recovery
      # defect, never normalized away by retrying or lowering worker count.
      # Keyed on the matrix step's own outcome: a pre-warm or tagged unit test
      # failure stops the job before any work dir exists, and a bare failure()
      # would add two misleading diagnostic failures on top (#6706 review).
          # graph-*.dump (#6162), not the two prior literal names: every cell
          # calls capture_digest <cell>, writing graph-<cell>.dump, so a
          # literal list only ever retained baseline/restartbackend and
          # silently dropped every other cell's dump (e.g. cell_expirelease in
          # run 34068313913).
```

## Docker Hub login commentary from main

      # #7886: true only when both Docker Hub secrets exist (fork and Dependabot
      # PRs get none). Expose the boolean, not the secrets, so no step sees them.
      # #7886: authenticated pulls avoid the shared anonymous Docker Hub limit
      # on hosted runners. Skipped, not failed, when the secrets are absent.
      # #7886: true only when both Docker Hub secrets exist (fork and Dependabot
      # PRs get none). Expose the boolean, not the secrets, so no step sees them.
      # #7886: authenticated pulls avoid the shared anonymous Docker Hub limit
      # on hosted runners. Skipped, not failed, when the secrets are absent.
      # #7886: true only when both Docker Hub secrets exist (fork and Dependabot
      # PRs get none). Expose the boolean, not the secrets, so no step sees them.
      # #7886: authenticated pulls avoid the shared anonymous Docker Hub limit
      # on hosted runners. Skipped, not failed, when the secrets are absent.
