# AGENTS.md — go/internal/cli/docs guidance for LLM assistants

## Read first

1. `go/internal/cli/docs/README.md` — purpose, ownership boundary, exported
   surface.
2. `go/internal/cli/docs/doc.go` — the godoc contract, including the exact
   filesystem read surface.
3. `go/cmd/eshu/docs.go` — the cobra `RunE` wrapper. It shows the split: flags
   in, `docs.Verify` called, exit code out.
4. `go/cmd/eshu/docs_image_api.go` — the other half of the wrapper, and the
   reason the image-truth decision is not in this package.

## Invariants this package enforces

- **No process wiring here.** No cobra flags, no `os.Getenv`, no `os.Exit`, no
  `fmt.Print*` to a fixed stream. `go/cmd/eshu` is `package main` and cannot be
  imported, so anything reading a flag, reading the environment, or choosing an
  exit code has to live in `docs.go` / `docs_image_api.go` instead. `rg
  'os\.Getenv|os\.Exit|cobra\.'` over this package returns nothing today; keep
  it that way.

- **Reads the filesystem, never writes it.** The read surface is enumerated in
  `doc.go` — six call sites across `InventoryDocuments`, `EnvironmentTruth`,
  `LocalPathResolver`, `LocalContainerImageResolver`,
  `TerraformAddressResolver`, and `TruthRoot`. There is no `os.Create`,
  `os.WriteFile`, `os.Mkdir*`, `os.Remove*`, `os.Rename`, or `os.OpenFile` in
  non-test code; every write-shaped call in this directory is a test writing
  into `t.TempDir()`. The only persistent writes are to Postgres through
  `Persistence.CommitScopeGeneration`.

  Before editing that list, **re-derive it** — `rg 'os\.[A-Z]|filepath\.(Walk|
  WalkDir|Glob|EvalSymlinks|Abs)'` over the package — rather than amending the
  sentence a reviewer complained about. Sentence-by-sentence patching of an
  extraction package's docs is how sibling packages ended up overclaiming
  through four review rounds.

- **An incomplete scan reports unsupported, never contradicted.** Every scan
  here returns a `(result, complete bool)` pair, and each resolver checks
  `complete` before calling an unmatched claim absent. This is the package's
  central accuracy rule: a file limit, an oversized file, an unreadable file,
  or unparsable HCL means the scan has not seen everything, and "I did not find
  it" is then not evidence that it does not exist. Breaking this turns correct
  documentation into a failing build.

- **The scans are lazy and run at most once.** `LocalContainerImageResolver`
  and `TerraformAddressResolver` each close over a `sync.Once`; the first claim
  triggers the walk and later claims reuse it. A resolver built for a document
  set with no image or Terraform claims never walks the tree at all.

- **`Deps.ContainerImageResolver` is supplied, not chosen here.** The
  local-versus-API decision needs `--service-url` / `--api-key` / `--profile`
  and `ESHU_SERVICE_URL` / `ESHU_API_KEY`, all of which are the wrapper's to
  read.

## Common changes and how to scope them

- **Add a new claim type** → add the resolver alongside the existing ones and
  wire it into the `doctruth.VerifierOptions` literal in `Verify`. Give it the
  same `(truth, complete)` shape so the unsupported-not-contradicted rule holds
  for it too.
- **Change what the image or Terraform walk skips** → edit
  `shouldSkipImageTruthDir` (imageref.go) or `shouldSkipTerraformTruthDir`
  (terraform.go). They are deliberately separate: the Terraform walk also skips
  `.terraform`, whose downloaded modules are not the workspace's own truth.
- **Change what makes a persisted run stale** → edit
  `InventoryFreshnessHint`'s `freshnessInput` and bump `freshnessVersion` in
  the same change. Without the bump, generations persisted under the old
  fingerprint shape keep matching and runs silently replay stale findings.
- **Add an environment reference page location** → `environmentReferenceCandidates`
  (inventory.go) is the single list.

## Failure modes and how to debug

- Symptom: a documented image or Terraform address reports `missing_evidence`
  when it plainly exists → the scan came back incomplete. Check the bounds
  (`imageTruthMaxFiles` / `terraformTruthMaxFiles`, and the 512 KiB per-file
  caps) and whether the file sits under a skipped directory. The resolver is
  behaving correctly; the scan is what fell short.
- Symptom: `--persist` re-commits on every run → the freshness hint is
  changing. It covers document revision ids *and* `--max-bytes`, `--limit`, and
  the effective image truth mode, so an invocation that varies any of those is
  a legitimately different generation.
- Symptom: `--persist` never re-commits after a real documentation change →
  the revision id should have moved. `readBoundedDocument` hashes the whole
  file even past `MaxDocumentBytes` precisely so a change past the bound still
  invalidates; a change there is the first place to look.
- Symptom: local path claims all report unsupported → `TruthRoot` found no
  workspace root, so `LocalPathResolver` returned nil.
  `eshulocal.ResolveWorkspaceRoot` looks for a `.eshu.yaml` then a `.git`
  marker in ancestor directories.

## Anti-patterns specific to this package

- **Wrapping the four `//nolint:wrapcheck` error returns.** They propagate to
  the CLI and are printed verbatim; wrapping changes operator-visible text for
  an otherwise unchanged failure. `go/.golangci.yml` exempts `cmd/` from
  wrapcheck but not `internal/cli/`, which is why the directives exist at all.
- **Reaching into `go/cmd/eshu`.** It cannot be imported. If new logic needs
  something only the CLI has, add a parameter or a narrow interface —
  `EnvelopeGetter` is the existing example.
- **Treating an unmatched claim as contradicted without checking `complete`.**
- **Reading the environment to decide the image truth source.** That decision
  belongs to the wrapper.

## What NOT to change without an ADR

- The unsupported-versus-contradicted rule. It is the difference between a
  documentation gate an operator can trust and one that fails on its own scan
  bounds.
- `freshnessVersion` semantics, or dropping a field from the freshness
  fingerprint. Both change which persisted generations are considered fresh,
  which silently changes what `--persist` replays.
- The `Persistence` interface shape. `go/cmd/eshu`'s tests implement it
  directly, and `PostgresPersistence` is the only production implementation.
