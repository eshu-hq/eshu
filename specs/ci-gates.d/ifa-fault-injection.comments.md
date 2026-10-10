# Comments moved from `ifa-fault-injection` gate

      # Glob, not the one filename: this file is the live-gate selector case
      # table and sits at 491 of the blocking 500-line cap, so it is itself a
      # split waiting to happen -- and a split of the table that proves these
      # gates select correctly is exactly the event #6200 is about.
      # The factschema root package, not a list of its filenames, for the
      # reason above (#6200). Every Decode* funnels through decodeMapIntoWith
      # and assignField in decode_map.go, every required-field rejection
      # through requiredPayloadKeys in fields.go, and the synth cassette both
      # gates generate is built by the encode_direct.go helpers -- none of
      # which any filename-shaped trigger named, all of which split out from
      # decode.go under the line cap. The per-family decode seams that sit in
      # this package root rather than a */v1/ subtree (decode_codeowners.go,
      # fact_kinds_codeowners.go, decode_documentation.go, decode_gcp.go, the
      # ArgoCD gitops decoder) were dark for the same reason, and this
      # supersedes #6198's *submodule*.go point fix. Go package scope is what
      # makes the package the right granularity: a helper any decode path
      # reaches can live in any file in it, so no name pattern bounds the
      # set. Measured: +1 armed commit in 300. A narrower set of seven
      # hand-aimed globs measured the same +1 and would have kept the
      # filename fragility, so it bought nothing.
      #
      # Sweeps in envelope.go, which #6200 excluded as already compile-pinned
      # by decode.go -- harmless, it was excluded as unnecessary rather than
      # unwanted -- and the import-extraction decoders, which #6200 excluded
      # because every parsed_file_data.imports bucket in every driven
      # cassette is empty. Editing those now arms two live lanes that cannot
      # fail on the change until that fixture gap is closed.
      # codegraph/v1 and gcp/v1 are separate packages, globbed for the same
      # reason and no wider: parsed_file_data_gitops.go (the ArgoCD struct the
      # decoder decodes into) and gcp/v1/resource.go and relationship.go were
      # dark beside the two codegraph filenames that were listed. Sibling
      # packages under sdk/go/factschema/ whose facts no driven cassette
      # carries -- aws/v1, azure/v1, sbom/v1 -- stay out on that ground and
      # only that ground. Not on reachability: 24 non-test .go files under
      # go/internal/reducer/ import aws/v1 and 4 import azure/v1, so their
      # code links into eshu-reducer like every other package this glob set
      # covers. What keeps them out is that nothing these gates drive decodes
      # an aws or azure fact: no aws_*/azure_* fact kind appears in any of the
      # 15 driven family cassette files (checked at this commit). Two aws_
      # tokens do appear, both payload data rather than a fact kind, and
      # neither reaches an aws/v1 decoder: Terraform source text inside a
      # content_body in the repodependency cassette, and an aws_account_id
      # attribute on a workload-identity-pool fact in the gcpcloud cassette,
      # which the gcp/v1 decoder reads. No azure_ token appears at all.
      # gcp/v1 is IN above for the mirror-image reason: the gcpcloud cassette
      # both gates generate carries 234 gcp_* occurrences. Both measured +0
      # armed commits in 300.
      # Every non-test file in this package compiles into the eshu-reducer
      # binary both live gates build and run, so any of them can change what
      # the gates prove. A hand-picked subset of ~40 filenames was an
      # under-approximation by construction (#6200): splitting a file to get
      # under the blocking 500-line cap left the original entry in place, so
      # nothing dangled and no drift check fired, while the new pieces went
      # dark. #6200's audit confirmed 26 load-bearing files dark that way,
      # and the glob covers far more than those 26 -- the whole
      # sql_relationships family past its two pinned filenames, the shared
      # projection helpers (admission_decisions.go, projection_helpers.go,
      # candidate_loader.go), and graph_projection_phase_publish.go plus the
      # repair runner that moved out of the still-listed
      # graph_projection_phase.go. This one entry replaces all of them, and
      # subsumes the earlier per-family point fixes (factschema_decode_*.go
      # among them). Six other gates in this registry already glob this
      # package. Measured cost before landing: over the last 300 merged
      # commits on main, at least one live lane armed on 160/300 before and
      # 169/300 after. Attributed one glob at a time against the same 300,
      # this one is +8 and sdk/go/factschema/*.go is +1; the rest are +0. The
      # harness is not blind -- seeding go/** the same way measures +106.
      #
      # It also arms the lanes on reducer files no driven cassette exercises,
      # code/semantic/materialization.go among them, which #6200 had
      # deliberately left untriggered because no cassette either gate drives
      # produces a semantic-entity intent. That is the price of the glob and
      # it is the right trade: per-file precision is what rotted.
      # The ifa package root, not a list of its filenames (#6200). go/cmd/ifa
      # -- which both live gates run for every drive and every
      # `ifa assert-edges` -- imports this package from its own production
      # files (coverage.go, expectations.go), so all 36 non-test files in it
      # compile into the binary the gates execute. 12 filenames and two
      # <family>_* globs were listed; 19 files were dark, and they were not
      # the peripheral ones: odu.go, catalog.go, expectations.go, coverage.go,
      # roundtrip.go and schema.go are the shared Odù machinery every family
      # goes through, and code_call_family_odu.go, rationale_family_odu.go,
      # sql_relationship_odu.go, repo_dependency_odu.go and
      # repo_dependency_backfill_odu.go are compiled Odù seeded into
      # catalogSeed. Weakening a derivation path in any of them re-ran no live
      # lane. Recorded on #6200 as its own instance of the same defect, and
      # fixed for every family at once rather than for the two that happened
      # to be noticed -- fixing some while others stay dark looks like
      # completeness without being it.
      #
      # Measured, same 300-commit method as the reducer and factschema globs:
      # +0 newly armed. 23 of those 300 commits touch this package root and
      # all 23 armed a live lane already, through a cassette, a script, or the
      # registry itself.
      #
      # What this does NOT fix: an Odù that drifts from its fixture is already
      # caught -- go/internal/ifa/testdata/<family>/** triggers both gates for
      # the families that have it, and ifa-contract-layer runs the
      # cassette/expected-edge cross-check. The dark case was a drift in BOTH
      # together, where the Odù and the expected edges move to agree on
      # something wrong.
      #
      # It sweeps in symbol_runtime_family_cassette.go, which the trio comment
      # below deliberately left out because LoadSymbolRuntimeFamilyOdu is
      # called only from a Go unit test. A package glob cannot carve one file
      # out of a Go package, and the carve-out was never sound anyway: file
      # membership, not call graph, is what decides whether an edit reaches
      # the binary. It also sweeps in amplify.go and slots.go (the
      # load-saturation surface) and mutate.go and dead_letters.go (the
      # dead-letter matrix's), for the same reason -- one package, one binary.
      # Glob though only canonical_codeowners_edges.go exists today: the
      # documentation family learned the hard way that a correct literal stops
      # being correct the moment the writer splits into a second file.
      # kubernetes_namespace_environment + iam_instance_profile_role (#6309):
      # mirror of the determinism block's family entries. The Go production
      # paths need no mirror: go/internal/ifa/*.go, go/internal/reducer/**,
      # and go/cmd/ifa/** above already cover the Odù builders, handlers, and
      # drive verbs. The writers are per-file literals on this gate (like the
      # canonical_* entries above), so they are named, not globbed.
      # handles_route/runs_in/invokes_cloud_action trio (#5995/#6000/#5997):
      # same cassette/Odù/expected-edges wiring as the determinism block
      # above -- see that block's comment for why the Go-test-only carve-out
      # it used to describe (symbol_runtime_family_cassette.go,
      # ifa_family_registry_anchor_test.go) no longer holds: both are inside
      # packages this gate now globs (#6200).
      # The trio's PRODUCTION paths. Without these, an edit to an extractor or
      # to a Cypher writer's MERGE identity does not re-run either live gate,
      # and the three now-unwaived coverage rows stay green on a stale live
      # proof -- the offline coverage gate is a projection-seam proof and never
      # exercises the graph write, so it cannot catch a MERGE regression either.
      # The trio's reducer-side extractors are no longer named here; the
      # go/internal/reducer/** entry above covers them, and every sibling
      # family, without depending on anyone remembering to add a filename
      # (#6200). The Cypher writers below still need naming: internal/storage/
      # cypher is not globbed by either gate, so each family enumerates its
      # own writer there (canonical_inheritance_edges.go, ...).
      # The scripts/lib/ Ifá surface as globs (#6200). This gate went from 68
      # literal scripts/lib/ filenames and two globs to 13 literals and 6
      # globs. Same defect as the reducer and factschema entries above, on a
      # directory the blocking 500-line cap splits far more often than any Go
      # package: across the four Ifá gates the registry named 78 scripts/lib/
      # files one at a time and carried only three globs there
      # (ifa_family_registry/**, ifa_family_registry_pins/** and this gate's
      # own ifa_fault_generic_*.sh), so every split dropped the new half out
      # of both live gates while the original filename kept its entry and
      # nothing dangled. This gate had two files
      # dark that way --
      # test-ifa-fault-injection-deployable-unit-kill-isolation-cases.sh and
      # test-ifa-fault-injection-generic-runner-lease-audit-cases.sh, both
      # split out under the cap, both absent from this registry AND from
      # ifa-determinism-gate.yml's paths: filter, so editing either started no
      # Ifá job at all. #6241 has since named both by hand; these globs close
      # the class rather than those two instances. ifa_fault_injection_driver.sh sources its cells by
      # `for f in .../ifa_fault_injection_*_cells.sh`, so the file set here is
      # open by construction and a name list can never close it.
      #
      # The two gates load DIFFERENT scripts/lib/ subsets and the split is
      # deliberate, so these globs preserve it: this gate gets ifa_fault_*.sh
      # and test-ifa-fault-injection-*.sh, ifa-determinism does not, and
      # ifa_determinism_common.sh stays a literal here rather than widening to
      # ifa_determinism_*.sh, because verify-ifa-fault-injection.sh never
      # sources the lifecycle module that glob would sweep in.
      # Four literals kept beside the two globs that already match them.
      # ifa-determinism names these same four as literals -- they are its
      # deliberate reach into the fault surface for the inheritance,
      # shell_exec and repo_dependency families -- and
      # ifa_live_gate_common_seams asserts one trigger STRING present in BOTH
      # gate blocks. Without the literals here that assertion has nothing to
      # match, since this gate covers them by glob and that gate must not be
      # widened to the whole fault surface.
      # kubernetes_namespace_environment + iam_instance_profile_role (#6309):
      # the first direct-materialization families with fault cells. Same
      # per-family literal treatment as the inheritance/shell_exec/
      # repo_dependency reach-ins above.
      # Every per-family live module this gate sources, plus the
      # deployable_unit diagnostics/converge pair that split off one of them.
      # Shared with ifa-determinism, which spells the same glob: ifa_sql_delta
      # _live.sh in particular is sourced here for cell_deltaretract (#5544),
      # so a change to it has to select BOTH gates.
      # The job installs ripgrep through this script before running the gate;
      # the gate's own preconditions shell out to rg. Without this trigger an edit
      # to the installer would not select the gate it is a precondition for.
      # #6162: the job pre-warms Go modules through this shared retry helper
      # before its first build/test step.
      # Sourced by the deployable-unit cases module rather than by the mirror
      # itself, which is how it went untriggered: editing it selected no gate
      # at all until this row existed.
      # #6147 PR-0 family-registry extraction: shared with ifa-determinism
      # above (sourced directly by verify-ifa-determinism.sh, transitively by
      # verify-ifa-fault-injection.sh via ifa_fault_generic_cells.sh).
      #
      # The row files carry every family's data; the orchestrator carries
      # none. Triggering only on the parent would leave a row correction
      # -- the most common edit -- selecting no gate at all.
      # Declared, existence-checked, sourced and run by the fault mirror on the
      # same source line as the runner-lease-hold module -- and it landed without
      # a row, so a PR editing only it selected no Ifa gate at all (#6161).
      # Restored as explicit entries alongside the broader globs above.
      # The broader globs cover these paths, but lockstep_test.go
      # pins the literal trigger text (slices.Contains, not a path match), and the
      # pin is the thing that stops a catalog or vacuity-guard edit from drifting
      # out of the live gates unnoticed. Keep both: the glob for coverage, the
      # explicit row for the pin (#6200).
      # Stem-bearing rows for symbol_runtime and sql_relationship.
      # TestEveryCoveredFamilyTriggersBothLiveGates requires each covered family
      # to have at least one trigger whose TEXT contains the family stem, so a
      # directory glob that covers the same files does not satisfy it. Keeping
      # these explicit is what ties the coverage row to a gate that re-runs (#6200).
      # The job runs as a four-shard matrix, so GitHub never emits a check
      # literally named "fault-injection" -- only "fault-injection (shard N/4)".
      # cmd/ci-gates/await.go falls back to the job name when check_names is
      # empty and then matches on exact string equality, so leaving this out
      # resolves the required check to MISSING forever: every PR touching an
      # ifa-fault-injection trigger path would be unmergeable, and worse, the
      # four shard checks that DO run would belong to no gate, so a red shard
      # would be invisible to required-gates-complete. Each name below is
      # validated against the expanded matrix by internal/cigates/drift.go, so a
      # name here that the matrix does not produce fails the drift check --
      # REMOVING a shard is caught that way. Adding a FIFTH shard is not: drift
      # validates check_names as a subset of the concrete names, and a new
      # concrete name nothing lists leaves it green. What catches an added shard
      # is the CHECK_NAMES CARDINALITY PIN in
      # scripts/lib/test-ifa-fault-injection-shard-cases.sh, which requires this
      # list to have exactly IFA_FAULT_SHARD_DEFAULT_N entries and every entry
      # to carry the /n denominator. Not the matrix-cardinality pin beside it --
      # that one compares strategy.matrix.shard against N and never reads these
      # names. Same subset pattern as golden-corpus-gate's corpus-gate
      # (nornicdb) above.
  # Owner row for the `static mirror` job of ifa-determinism-gate.yml (#7807).
  # `ci-gates await` waits only for checks a row names, so before this row that
  # job could fail inside a merge group and nothing blocked the merge: it
  # failed on 63 runs between 04:20 and 11:12 UTC on 2026-10-09 (26 merge
  # groups, 15 pushes, 22 pull requests) while required-gates-complete stayed
  # green, and was fixed by #7830. It passed in all 43 runs before that streak
  # and in every run after the fix, so it is deterministic. The job
  # runs the three static mirrors that are the local.command of
  # ifa-determinism, ifa-dead-letter-matrix and ifa-fault-injection, so this
  # row is CI-only and its triggers are the union of those three rows: a path
  # that arms any live Ifa gate arms its mirror. The mirrors read production
  # sources (for example reducer_queue_replay.go) as text, so a production edit
  # must select the mirror, not only the live cells. The registry/workflow
  # lockstep (scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh)
  # pins both that this list stays a superset of the three rows' triggers and
  # that the workflow lists every entry.
  #
  # The row is non-blocking by owner decision (2026-10-09): the mirrors are
  # hand-kept shell copies of production values, so a production edit that
  # forgets the copy fails the job while production stays correct (the 63
  # failures above). The job still runs, still shows red on the pull request,
  # and an owner row keeps it from being an orphan, but its failure does not
  # hold a merge or make main red. Flip to blocking once the mirrors derive
  # their expectations from the production sources instead of copying them.
