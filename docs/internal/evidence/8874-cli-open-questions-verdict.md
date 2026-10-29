# #8874 open-questions arbiter verdict

Refs #8874, part of #6707. Status: decision record only. This file
records the landed arbiter ruling on the three open questions the
step-1 census carried as NOT_CHECKED. It moves no code, authorizes no
release, deployment, rollback, or in-tree removal.

Source: arbiter-eshu ruling, 2026-10-28 (REQUIRE-DEST-CUTOVER
close-out basis). Base: `origin/main` `9922b49`. Census re-pin at this
base: `go/cmd/eshu/` holds 146 entries (the stale cutover worktree
listed 145; the count is base-dependent and is re-pinned here, not in
the brief).

## Q1 — Supported core<->CLI version pairs and credential handling

1. One tested pair at first independent release; no support claim
   before it. Until `eshu-cli` cuts its own tag, no supported pair
   exists. The destination contract's same-tree
   `Eshu <buildinfo.AppVersion()>` row describes today's in-tree
   build, never a support pair, and must be replaced with real tags
   at release time.
2. Independent version lines. Core keeps root `vX.Y.Z` tags;
   `eshu-cli` versions independently with its own SemVer tags in its
   own repo, injected through the existing `buildinfo.Version`
   ldflags mechanism. After extraction `eshu --version` prints the
   CLI's version, never core's. The pair in force is reported, not
   guessed: core already exposes `"version": buildinfo.AppVersion()`
   in its status payload (`go/internal/query/status.go`), so CLI
   `doctor` / `version --json` surface `{cli_version,
   server_version}` with no new server endpoint.
3. The wire contract is the pair unit, not the release train: `/api/v0`
   plus `application/eshu.envelope+json` plus exit codes 0-6. While
   both sides are pre-1.0 there is no cross-minor guarantee; the
   supported line is the latest CLI minor. A wire-major bump adopts
   the SDK's existing N/N-1 dual-accept window and deprecation
   announcement rules instead of inventing CLI-specific policy.
4. Single home for the matrix: eshu-deploy
   `docs/compatibility-matrix.md` (currently a v0 single-pair for
   core/chart, no CLI or demo rows yet). `eshu-cli` docs link to that
   matrix; the CLI repo never carries a copied table that can fork.
   Widening the range requires a tested pair merged there first.
5. No new runtime version enforcement on day one. Byte-identical
   help/JSON/exit behavior is the extraction contract. The CLI fails
   closed only on signals that already exist: envelope
   `unsupported_capability` (exit 6) and component
   `spec.compatibleCore` through `--core-version` /
   `ESHU_COMPONENT_CORE_VERSION`. A client-side range reject or
   warning is a behavior change and waits for the first wire-major
   bump. `dev` builds stay unsupported.
6. SDK pins: exact published versions on original module paths
   (`sdk/go/collector` and `sdk/go/factschema` v0.2.0, proxy-verified;
   the separate `eshu-sdk` paths are candidates, not yet
   authoritative). `eshu-cli` pins exact original-path versions with
   no `replace` and no `go.work`, repoints imports to public SDK
   surfaces only, and does not switch to `eshu-sdk` module paths
   inside #8874.
7. Credential storage moves with the CLI. `~/.eshu/.env` written 0600,
   resolution order flags -> profile `<KEY>_<PROFILE>` -> env ->
   default (`ResolveValue`), sent as `Authorization: Bearer`.
   `ESHU_HOME`, the `.env` location, and its line format are a frozen
   cross-repo filesystem contract. Core must not read or write that
   file after extraction: retained core `admin initial-credential`
   hands off by printing the credential or through environment,
   never by writing a CLI profile.
8. No credential material anywhere else. No secrets in either repo,
   in release artifacts, in demo assets, or in evidence output;
   `evidredact` and its redaction canary tests move with the CLI.
   `ESHU_MCP_TOKEN` stays an env reference in IDE snippets, never
   persisted. `--api-key` stays for parity but docs mark profile/env
   as preferred (documentation only, no behavior change). No
   OS-keychain work in #8874.
9. Release blocker named now: `eshu-cli` must name
   vulnerability-response ownership (`SECURITY.md` in `eshu-cli`)
   before its first independent release. This blocks the release,
   not the population PR.

## Q2 — Demo Compose overlay and manifest distribution (joint with #8871)

10. The question manifest and the Compose overlay are separate
    distribution problems with different owners.
11. Manifest: embed in `eshu-cli`. `specs/demo-first-answers.v1.yaml`
    (version string `demo-first-answers/v1`) ships as a `//go:embed`
    copy pinned to `demo-first-answers/v1`. Lookup order: an on-disk
    `specs/demo-first-answers.v1.yaml` relative to the working
    directory wins (preserving today's in-checkout behavior byte for
    byte); otherwise the embedded copy is used. `eshu-cli` CI diffs
    the embedded bytes against core's spec at the paired core tag
    (fetch in CI only, never at runtime).
12. Compose overlay: deploy-repo checkout only. No embed and no
    runtime pinned fetch in #8874, for three independent reasons,
    each sufficient: (a) single owner — Compose assets and the
    tested CLI<->Compose<->core-image triple belong to eshu-deploy
    under #8871; embedding duplicates that owner and freezes
    digest-pinned images to the CLI's release cadence; (b) the
    overlay is a tree, not a file — the wrapper `include:`s
    corpus/runtime fragments, the runtime fragment `extends: file:
    docker-compose.yaml`, and the corpus fragment mounts
    `./tests/fixtures/ecosystems`,
    `./scripts/demo-corpus-staging.sh`, and `./testdata/cassettes/*`
    with `build: context: .`; fetching YAMLs cannot run this, and
    fetching a repo subtree at runtime invents a new
    code-distribution and supply-chain mechanism inside an
    extraction; (c) the fixture/cassette/staging paths are absent
    from eshu-deploy's tree today, so any embed or fetch shipped now
    would distribute a broken demo.
13. Installed-CLI behavior day one: `demo up/status/down` resolve the
    overlay exactly as today — `ESHU_DEMO_COMPOSE_FILE` override
    first, else walk-up search for `docker-compose.demo.yaml` — which
    in practice means an eshu-deploy checkout. A miss keeps failing
    with guidance; the one sanctioned parity exception is updating
    the miss text to name an eshu-deploy checkout and the paired
    deploy tag from the compatibility matrix, recorded as a
    deliberate contract change, never silent drift.
14. What waits on #8871: making the deploy-side demo bundle
    self-contained (relocating or replacing the corpus
    fixture/cassette/staging references with versioned artifacts,
    digest-pinning images) and publishing it as one versioned
    artifact with checksums and provenance. Only after that may a
    follow-up add an optional pinned fetch as a third resolver tier
    (download exactly the bundle version in the CLI's tested triple,
    verify sha256, cache under `ESHU_HOME`). No fetch code lands in
    extraction PRs. Until the triple lands in eshu-deploy's matrix,
    "demo outside a checkout" stays NOT_CHECKED by construction.

## Q3 — Future core `local-host` entrypoint (name, versioning, discovery)

15. Name: `eshu local-host` in the core `eshu` binary. No new binary,
    no new repo. The hidden subtree already exists with `watch` and
    `mcp-stdio`. The cross-repo contract names the protocol, never a
    binary path.
16. Versioning: explicit protocol string `local-host/v1`, following
    the repo's existing pattern (`collector-sdk/v1alpha1`,
    `demo-first-answers/v1`). Core adds an additive handshake
    returning `{protocol, core buildinfo version, capabilities}`.
    Support rule mirrors the SDK N/N-1 window: the CLI speaks
    protocol N and N-1; unknown or newer protocol fails closed with
    `unsupported_capability` (exit 6) and upgrade guidance, never a
    guessed invocation.
17. Discovery tier 1 (binary): `ESHU_LOCAL_HOST_BIN` env override
    (new entry in the central env registry), then `eshu` on `PATH`,
    then fail with guidance naming the core install. The handshake
    runs before any verb executes.
18. Discovery tier 2 (running host): freeze the layout, redact the
    record. The `${ESHU_HOME}/local/workspaces/<id>/` layout,
    workspace-ID derivation, and record location become a recorded
    filesystem contract. But today's `OwnerRecord` must NOT become
    the cross-repo contract: it carries `GraphPassword` in plaintext
    plus Postgres PID/port/data-dir/socket handles. Core instead
    publishes a separate redacted v1 discovery record alongside
    `owner.json`: protocol, core version, PID, started_at,
    workspace_id, health endpoint/socket, profile, graph backend
    name. No passwords, no data dirs, no DB ports. `owner.json`
    stays core-internal forever.
19. Verb mapping once v1 exists: `graph start` and `watch` exec the
    discovered core binary with today's exact argv
    (`local-host watch <workspace-root>`); core v1 adds
    `local-host status|stop|logs` subcommands and the CLI becomes a
    thin exec with byte-passthrough stdout and exit codes while
    supervisor logic stays core-owned. The CLI reimplements only
    layout-path derivation and redacted-record parsing from the
    published v1 spec — it never imports `eshulocal` or
    `localsupervisor`.
20. Until core ships `local-host/v1`, `graph`/`watch`/`w` stay RETAIN
    in core. The first `eshu-cli` release ships STUB entries for them
    using the existing `removedCommandError` pattern (exit 1,
    guidance pointing at the core binary), so an installed-CLI user
    gets a supported-contract pointer instead of cobra's "unknown
    command". This absence is a recorded parity exception, same
    class as the demo miss-text update in decision 13.

## Sequencing

21. Nothing above blocks the `eshu-cli` population PR (EXTRACT/STUB
    families plus owned `go/internal/cli`, imports repointed to
    public SDK only). The destination holding zero Go source is the
    critical path; all three questions are now decided at contract
    level.
22. First independent `eshu-cli` release waits on: parity proof, one
    tagged core release forming the v0 pair, the pair merged into
    eshu-deploy's matrix, `eshu-cli` `SECURITY.md` naming
    vulnerability-response ownership, and rollback rehearsal
    (reinstall prior CLI artifact; the only durable-state boundary
    is the unchanged `~/.eshu/.env` format).
23. Demo for installed users waits on #8871 (self-contained,
    versioned bundle). The manifest embed (decision 11) may land
    with population.
24. `graph`/`watch` in `eshu-cli` wait on core `local-host/v1`
    (handshake plus redacted record plus status/stop/logs
    subcommands, all additive in core).
25. In-tree removal waits on all of the above plus production
    acceptance, per the migration-plan cutover gate; #8874's
    unchecked acceptance items (credential/pair record, independent
    build/release, rollback, production acceptance) close in that
    order, not before.

## Required proof (population and release)

- Parity: destination `scripts/check-parity.sh` plus representative
  and hostile inputs across `query`, `map`, `trace`, `change`,
  `scan`, `report`, `service-report`, and `vuln-scan
  provider-parity`, checking help bytes, `--json` envelopes,
  truth/redaction fields, and exit codes 0-6 against the in-tree
  binary. The only sanctioned diffs are decisions 13 and 20.
- Version pair: from a scratch directory outside any checkout,
  install the tagged CLI artifact, run it against the paired core
  tag, paste `version --json` / `doctor` output showing both
  versions; the same run against an unpaired core is recorded as
  unsupported, not as a failure of the pair.
- Credentials: profile round-trip test (write profile -> 0600 mode
  asserted -> `ESHU_API_KEY` resolved through profile suffix ->
  Bearer [REDACTED] observed at a stub server), plus redaction
  canaries green in `eshu-cli` CI; `rg` census showing no
  `go/internal/` imports and no credential material in the
  destination tree.
- Demo manifest: `eshu-cli` CI byte-diff of the embedded manifest
  against core's spec at the paired tag; `demo` guidance-render run
  from a temp dir outside any checkout proving the embedded
  questions load.
- Demo Compose: from an eshu-deploy checkout at the matrix tag, an
  `ESHU_DEMO_COMPOSE_FILE` run of `demo up` answering the five
  manifest questions; a miss run outside any checkout proving the
  updated guidance names eshu-deploy. Self-contained-bundle proof
  belongs to #8871 and is not claimed here.
- Local-host (when v1 lands): handshake transcript for matching, N-1,
  and too-new protocols (last must exit 6); golden-fixture test
  that the CLI's layout derivation and redacted-record parser match
  core's published spec fixtures; canary test proving no
  password/DB-handle field name appears in the v1 record schema.
- Rollback: reinstall the previous CLI artifact over the new one
  against the same `~/.eshu/.env` and paired core, with read-truth
  spot checks before/after.

## Will not do (and why)

- No runtime fetch mechanism for Compose assets in #8874: it would
  invent an unversioned distribution channel for code that mounts
  host paths and builds images, inside an extraction whose contract
  is byte parity.
- No embedding of Compose YAML in the CLI: duplicates eshu-deploy's
  ownership and freezes digest pins to the wrong release cadence.
- No publication of `OwnerRecord` as a contract: it contains a
  plaintext graph password and database handles.
- No new `eshu-local-host` binary or repository: core-only utilities
  are retained-core utilities under the approved migration plan,
  and a binary split is not needed to version a protocol.
- No client-side core-version range enforcement or warning on day
  one: any new stderr/exit behavior breaks the parity contract that
  is the whole safety basis of the extraction.
- No OS-keychain credential store and no `eshu-sdk` new-path module
  switch inside #8874: both are real future work, neither is
  required by the extraction contract, and bundling either widens
  the cutover's failure surface.
- No copied compatibility table in `eshu-cli`: one matrix, in
  eshu-deploy, or the copies will diverge.
- No serialization of any kind is introduced or needed: every
  decision above is a contract boundary, not a concurrency change.

## NOT_CHECKED in this ruling

- Whether eshu-deploy's current demo Compose set actually runs
  end to end given the missing `tests/fixtures`,
  `scripts/demo-corpus-staging.sh`, and `testdata/cassettes` paths
  in its tree — established by tree listing only; no `docker
  compose` run was performed (read-only ruling).
- Whether core's status/version payload is reachable without
  credentials for a pre-auth pair probe — the field's existence in
  `go/internal/query/status.go` is verified; its auth posture was
  not traced.
- The exact core tag that will form the v0 pair — a release-time
  fact; the deploy matrix's current core pin (`v0.0.3-pre-release-18`)
  is a pre-release and must not be silently promoted to the CLI pair.
- Any performance claim: the extraction is contract-preserving, so
  no benchmark was run; if population changes import structure in
  ways that affect startup latency, before/after `eshu --help` and
  a representative `query` timing must be measured in the
  population PR, not assumed.
- Independent release, deployment, rollback rehearsal, and
  production acceptance for `eshu-cli` remain unmet by construction
  (destination has no Go source yet), consistent with the
  destination contract's own NOT_CHECKED list.
