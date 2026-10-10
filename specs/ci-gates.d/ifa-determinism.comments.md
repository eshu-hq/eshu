# Comments moved from `ifa-determinism` gate

      # The pattern this mirror scans with, and its positive control. Both
      # mirrors source it; without a row here, neutering the pattern re-runs
      # nothing (#6161).
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
      # Glob though only canonical_codeowners_edges.go exists today: the
      # documentation family learned the hard way that a correct literal stops
      # being correct the moment the writer splits into a second file.
      # kubernetes_namespace_environment / iam_instance_profile_role (#6228):
      # the first DIRECT-materialization families in a live gate. Each carries
      # its own Odù builder, committed cassette and expected-edge directory,
      # plus the reducer handler and cypher writer whose output the exact-set
      # assertion pins -- a change to any of them must re-run the gate that
      # proves the family, or its coverage row keeps asserting a proof that has
      # gone stale. ifa_direct_family_live.sh supplies both families'
      # drive/assert callbacks, so it is listed once for both.
      #
      # Mirrored onto ifa-fault-injection since #6309 landed the cells: the
      # fault block carries the cassette/testdata/writer/cells literals, while
      # the Go production paths need no mirror (covered by broader globs on
      # both gates). Before the cells existed this block deliberately stayed
      # determinism-only so the rows would not read as fault-covered while
      # nothing drove them; now that both families have fault cells, the six
      # seams live in the shared selector table (ifa_live_gate_common_seams),
      # which the lockstep loop proves trigger-by-trigger: each seam's trigger
      # appears in BOTH gates' registry blocks and its concrete path selects
      # BOTH gates through the real `ci-gates select` matcher. The six
      # of these thirteen that are not already matched by a broader trigger
      # are pinned there, so the real matcher proves the split rather than
      # this comment asserting it.
      #
      # SCOPE CAVEAT: the per-gate split above is REGISTRY-level. It holds for
      # `ci-gates select` and `make pre-pr`, which consult these per-gate
      # trigger lists. It does NOT hold in GitHub Actions, because
      # .github/workflows/ifa-determinism-gate.yml carries one shared
      # `on.pull_request.paths` filter covering the union of all three gates'
      # triggers, with no per-job `if:`/`paths`. So editing any path above
      # still starts the four-shard fault-injection matrix in CI -- which,
      # since #6309, DOES observe these families through their fault cells.
      # That over-triggering is deliberate workflow policy (under-triggering
      # ships a dark gate, #6200), not a gap in the registry split; narrowing
      # it needs a per-job condition in the workflow, not a registry change.
      # handles_route/runs_in/invokes_cloud_action trio (#5995/#6000/#5997):
      # one shared cassette and Odù (symbolRuntimeFamilyOdu() is registered
      # in catalog_seed.go's catalogSeed -- live-binary-consumed, not
      # test-only), three SEPARATE expected-edge directories (one per
      # family's own exact-set assertion). symbol_runtime_family_cassette.go
      # used to be excluded here on the grounds that
      # LoadSymbolRuntimeFamilyOdu is called only from a Go unit test; the
      # go/internal/ifa/*.go package glob above now covers it, and the
      # sibling registry-shape Go tests that same reasoning excluded
      # (materialized_edge_family_blocker_shape_test.go,
      # ifa_family_registry_anchor_test.go) sit in go/internal/reducer/ and
      # are covered by that package's glob (#6200). Call-graph reachability
      # was the wrong test: what decides whether an edit can reach the binary
      # is which package the file is in.
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
      # The scripts/lib/ Ifá surface as globs (#6200). This gate went from 34
      # literal scripts/lib/ filenames and two directory globs to 10 literals
      # and 8 globs. Same defect as the reducer and factschema entries above,
      # on a directory the blocking 500-line cap splits far more often than
      # any Go package: across the four Ifá gates the registry named 78
      # scripts/lib/ files one at a time and carried only three globs there
      # (ifa_family_registry/**, ifa_family_registry_pins/** and, on the fault
      # gate, ifa_fault_generic_*.sh), so every split dropped the new half out
      # of both live gates while the original filename kept its entry and
      # nothing dangled. Two files went dark exactly that way -- test-ifa-
      # fault-injection-deployable-unit-kill-isolation-cases.sh and test-ifa-
      # fault-injection-generic-runner-lease-audit-cases.sh, both split out
      # under the cap, both absent from this registry AND from
      # ifa-determinism-gate.yml's paths: filter, so editing either started no
      # Ifá job at all. #6241 has since named both by hand; these globs close
      # the class the enumeration kept reopening, not those two instances.
      #
      # The two gates load DIFFERENT scripts/lib/ subsets and the split is
      # deliberate, so these globs preserve it rather than collapsing both
      # gates onto one directory-wide pattern: verify-ifa-determinism.sh
      # sources no *_cells.sh file for any family, and the fault mirror
      # sources no test-ifa-determinism-* module. ifa-fault-injection gets
      # ifa_fault_*.sh and test-ifa-fault-injection-*.sh; this gate does not.
      # The six cross-family fault literals below stay literal for the same
      # reason -- widening them to a glob would hand this gate the fault
      # gate's whole surface.
      # Every per-family live module verify-ifa-determinism.sh sources, plus
      # the deployable_unit diagnostics/converge pair that split off one of
      # them. Both halves of that split were named here; the next one would
      # not have been.
      # #6162: the determinism-matrix job pre-warms Go modules through this
      # shared retry helper before its first build/test step.
      # kubernetes_namespace_environment + iam_instance_profile_role (#6309):
      # same deliberate reach into the fault surface as the inheritance/
      # shell_exec literals above.
      # #6147 PR-0 family-registry extraction: shared with ifa-fault-injection
      # below (sourced directly by verify-ifa-determinism.sh, transitively by
      # verify-ifa-fault-injection.sh), plus this mirror's own sourced case
      # modules -- determinism-only, since none of them execute inside the
      # fault-injection gate.
      #
      # The row files carry every family's data; the orchestrator carries
      # none. Triggering only on the parent would leave a row correction
      # -- the most common edit -- selecting no gate at all.
      # The per-family pin files the module above sources by variable path.
      # Their hard rule is that pins are hand-derived and never generated
      # from the registry; editing one to "just make it match" must re-run
      # the gate that would object.
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
