# 8874 population-scope arbiter verdict (amendment)

Refs #8874, part of #6707. Status: decision record only. This file
records the landed second arbiter ruling on the eshu-cli population
scope. It amends
`docs/internal/evidence/8874-cli-open-questions-verdict.md`: decision 21
of that verdict ("nothing above blocks the population PR") is superseded
by the tiered scope below. Decisions 1-20 and 22-25 stand unchanged; they
are contract-level (version pairs, credentials, demo, `local-host/v1`)
and none depends on the import closure.

Source: arbiter-eshu ruling, 2026-10-29 (population scope).
Base: `origin/main` `37171bafa2` (merge of #9504).
Skills loaded for the ruling: `eshu-contract-rigor`.
Census basis: `go list -deps` on this checkout (decisive route),
corroborated by `git grep` (local `rg` is 15.1.0, below the 15.2.0
census floor, so no ruling point rests on `rg` alone).

## Evidence the ruling verified

1. The 499 figure is exact: `go list -deps ./internal/cli/docs
   ./internal/cli/report ./internal/cli/vulnscan ./internal/cli/scan`
   counts 499 `eshu/go/internal` packages (full `./cmd/eshu` closure:
   576 internal of 1205 total).
2. Two named roots, not uniform entanglement: `go/internal/query`
   closes over 452 internal packages, `go/internal/mcp` over 495.
   `cli/report` reaches 456 via `query`+`reportbundle` (455);
   `cli/mcpsetup` reaches 496 via `mcp`; `cli/evidredact` reached 498
   through one call, `mcpsetup.RedactToken`, which dragged
   `doctor`/`firstrun`/`hosted` to ~500 through it.
3. A genuinely small subset exists: `cli/trace` 1, `cli/apierr` 1,
   `cli/playbooks` 1, `cli/change` 2, `cli/entitymap` 2,
   `cli/freshness` 2, `cli/config` 2, `cli/evidpacket` 2,
   `cli/scan` 3 internal packages each.
4. The impure uses are substantive, not incidental imports: `docs
   verify` runs the `doctruth` engine and opens Postgres (`docs.go`,
   `cli/docs/persistence.go`); `service-report` composes locally via
   `serviceintel.Compose`; `investigation export` renders locally via
   `query.RenderInvestigationPacket`; `vuln-scan` local mode starts and
   supervises a local owner plus API via `localsupervisor`.
5. The shared seam file blocks even the pure subset as-is:
   `go/cmd/eshu/basic.go` imports `eshulocal` for `runWatch`
   (`ResolveWorkspaceRoot`) in the same file/package as the
   pure-remote `runList`/`runStats`/`runQuery`. `runIndex` execs
   `eshu-bootstrap-index` from `PATH`, a core-binary dependency the
   inventory's intake-contracts row does not state.
6. The SDK has no equivalent surface: only `sdk/go/collector` and
   `sdk/go/factschema` v0.2.0 are qualified; `ResponseEnvelope` /
   `TruthEnvelope` / `QueryProfile` live in
   `go/internal/query/querycontract` behind aliases, unpublished.

## Decisions

1. **Chosen option: A as the backbone, C sequenced behind it, B
   rejected.** B (fork the internals) violates "each capability has
   exactly one implementation owner" and "shared response or request
   shapes belong to an explicit public contract"
   (`docs/internal/design/eshu-ecosystem-repository-migration.md`).
   Copying 499 packages to evade "public modules must not import
   `go/internal/...`" honors the letter and breaks the rule, and would
   give the CLI a second, diverging copy of truth-bearing logic plus a
   direct Postgres handle: an accuracy failure by construction.
2. **Population scope = Tier 1 only** (pure-remote/mechanical, small
   closure, no core truth logic): `analyze`, `find`, `query`,
   `list`/`ls`, `stats`, `index-status`, `change`, `map`, `trace`,
   `freshness`, `playbooks`, `ecosystem overview`, `completion`,
   `help`, `version`, `neo4j` guidance, all STUBs, plus Tier-3 stubs
   per decision 5. Each family's Tier-1 membership is proven by its
   closure census in the PR, not by this list.
3. **Tier 2 gates on a new published API wire-contract module in
   `eshu-sdk`** (envelope/truth/profile shapes from `querycontract`,
   tool-definition shape for `mcp setup`, env-registry data for
   `config validate`). Shapes only; publishing rendering/composition
   logic or storage ports is forbidden. This module gets its own row
   and qualification in `sdk-compatibility.md`; until a pair
   qualifies, `report`, `investigation`, `mcp setup`, `config
   validate`, and component client verbs that use `component` core
   locally stay out of the population. Where the seam is type-only
   (e.g. `report`'s `ResponseEnvelope` decode target), Tier 2 is
   sufficient; where local logic remains (`RenderInvestigationPacket`,
   `serviceintel.Compose`, `reportbundle.Capture/Validate`), the
   family additionally waits for a core endpoint.
4. **Tier 3 stays in core** until a core endpoint or `local-host/v1`
   exists: `docs verify` (core truth engine + Postgres; core-only
   utility retention), all local-owner modes of `scan`/`vuln-scan` and
   `service` local profiles, and `answerquality`-backed scoring.
   Leaf scorers with closure 1 and no non-CLI production consumer
   (`competitiveparity`, `packetdogfood`, `vulnerabilityparity` /
   `vulnerabilityparityproof`) may **move** (single-owner transfer)
   only after the PR's sole-consumer census proves it per package; a
   move, never a fork.
5. **New recorded parity exceptions** (same class as verdict decisions
   13 and 20, enumerated, no blanket waiver): **E3** -- every
   Tier-2/Tier-3 family ships in the first population as a STUB via the
   existing `removedCommandError` pattern (exit 1, pointer to the core
   binary); **E4** -- `index` keeps its `PATH`-exec of
   `eshu-bootstrap-index` as a recorded binary-discovery contract
   parallel to decision 17, with the build-from-source miss text
   updated to name the core install; **E5** -- `--help` bytes for each
   stubbed family differ and are listed family by family in the PR.
   These plus decisions 13 and 20 are the only sanctioned diffs.
6. **Sequencing:** S1 core seam PRs first, behavior-identical and
   parity-tested in-tree (split `basic.go` so pure-remote verbs carry
   no `eshulocal` import; break the `evidredact -> mcpsetup.RedactToken`
   edge by moving that one function to the redaction owner). S2
   publish the Tier-2 wire-contract module. S3 population PR (Tier 1 +
   stubs). S4 core endpoints for Tier-3 logic, each family separately
   decided (endpoint vs. retain-forever). S5 release gates per decision
   22, unchanged. No step runs ahead of its gate.
7. **Moved-package freeze:** until in-tree removal, any package copied
   to `eshu-cli` is pinned to a named core commit and CI-diffed
   against it (the decision-11 manifest-embed pattern). Divergent
   evolution in either tree is a fork under decision 1 and blocks
   release.

## Required proof in the population PR

1. **Closure census (machine-checked):** exact `go list -deps`
   command and count per populated family; `git grep` (or `rg`
   >= 15.2.0) census showing zero `go/internal/` imports and no
   credential material in the destination; `go.mod` with exact
   published pins, no `replace`, no `go.work`. Any populated closure
   containing `query`, `mcp`, `serviceintel`, `doctruth`,
   `component`, `storage`, `eshulocal`, or `localsupervisor` fails by
   construction.
2. **Sole-consumer census** for every moved package (production
   callers across core); shared owner implies publish the shape or
   retain.
3. **Parity:** destination `scripts/check-parity.sh` over Tier-1
   families (help bytes, `--json` envelopes, truth/redaction fields,
   exit codes 0-6) with E3/E4/E5 plus decisions 13/20 as the only
   diffs, each diff shown.
4. **S1 split proof:** in-tree parity (help/JSON/exits) before vs.
   after the `basic.go` split and the `RedactToken` move, run in core
   before population.
5. **No-fork diff:** moved files byte-identical to the pinned core
   commit except import paths, demonstrated by script output.
6. **Performance:** before/after `eshu --help` and one representative
   `query` timing (carried from the first verdict's NOT_CHECKED list);
   **credentials** proof per that verdict's decision 7, unchanged.

## Will not do

- No fork, vendor, `replace`, or `go.work` route around the import
  rule (decision 1).
- No SDK publication of internal storage, reducer, graph, queue, or
  supervisor surfaces.
- No big-bang C inside the population PR: inventing core endpoints for
  `docs verify` / packet render / service compose is behavior work
  with its own proof, and bundling it destroys the byte-parity basis.
- No population of a family on its inventory EXTRACT label alone; the
  inventory classified intent, the closure census classifies fact.
- No serialization is introduced or needed; every gate above is a
  contract boundary.

## NOT_CHECKED in this ruling

- Whether any Tier-1 candidate hides a local fallback beyond its
  import census (the `service.go` local-authoritative profiles show
  the pattern exists); the PR's per-family runtime trace must settle
  this.
- `eshu-cli` destination state beyond the inventory's "README-only"
  note; not re-read for this ruling.
- Whether `mcp setup`'s offline snippet generation can be served by
  remote `tools/list` instead of a published shape; S2 must decide
  with evidence.
- `doctruth`'s non-CLI consumers (`query`, `workflowimage`) confirm
  core ownership, but no endpoint design for `docs verify` was
  evaluated; retain-forever remains a legitimate S4 outcome.
- No benchmark was run for this ruling; proof item 6 assigns it to
  the population PR.

## S1 landing record

S1 (sequencing decision 6, first gate) landed as the seam commit on
`drive/8874-s1-core-seams` before this amendment was recorded:

- `runWatch` moved `go/cmd/eshu/basic.go` -> `go/cmd/eshu/watch.go`
  (new file, same `package main`); `basic.go` carries no `eshulocal`
  or `procexec` import. Command registrations untouched.
- `RedactToken` moved `internal/cli/mcpsetup` ->
  `internal/cli/evidredact` (`token.go`, with `token_test.go` moved
  alongside); `mcpsetup.RedactToken` stays as a deprecated alias;
  `evidredact/endpoint.go` no longer imports `mcpsetup`.
- In-tree parity proof: root plus per-command `--help` bytes
  (`query`, `list`, `stats`, `index-status`, `watch`, `index`, `map`,
  `trace`, `change`, `freshness`) byte-identical before/after.
- Focused proof: `go vet` clean on `cmd/eshu`, `evidredact`,
  `mcpsetup`, `urlredact`; `go test` green for `cmd/eshu` and all
  seven touched/consumer packages (`evidredact`, `mcpsetup`,
  `urlredact`, `doctor`, `hosted`, `firstrun`).
- Closure census: `go list -deps ./internal/cli/evidredact` shows
  only `urlredact` plus itself; `basic.go` has no `eshulocal` /
  `procexec` reference.
- Full `go build ./...` was NOT_CHECKED for this lane: cgo linking
  of unrelated heavy binaries (`collector-grafana`,
  `bootstrap-index`, `golden-corpus-gate`, `ifa`) exceeds the shared
  host's `/tmp` quota. The shipped binary (`cmd/eshu`) builds and
  the affected packages vet clean.
