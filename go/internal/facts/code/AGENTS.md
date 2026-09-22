# AGENTS.md — internal/facts/code

Scoped instructions for this package. Read `README.md` and `doc.go` first;
the root `AGENTS.md` and `CLAUDE.md` still apply.

## Invariants

- **Must never import `go/internal/facts`.** Same one-way rule as `cloud`
  and `encode`: the facts root's transitional `compat_code.go` already
  imports this package, so the reverse import cycles.
- **These kinds are unversioned by design.** Do not add a schema-version
  constant or a `specs/fact-kind-registry.v1.yaml` entry for a kind here
  without first confirming with the owner that the kind is joining the
  versioned-admission regime — that is a deliberate, tracked change (see
  the registry file's own comment on "version-less git kinds"), not a
  default to apply to every new kind.
- **No package-name stutter and no destuttering regressions.** Exported
  names are `code.TaintEvidenceFactKind`, not
  `code.CodeTaintEvidenceFactKind` (`docs/internal/naming.md` rule 4). Do
  not reintroduce a `Code` prefix on a new export in this package.
- **`FlowReadFactKinds()` lockstep.** Adding, removing, or reordering a
  kind in this function requires updating
  `codemodel.ListActiveCodeFlowFactsSQL`'s literal `fact_kind IN (...)`
  list (`go/internal/query/codemodel/code_flow_postgres.go`) and the
  `fact_records_code_flow_repo_idx` partial index predicate in the same
  change, or the query/postgres lockstep guard tests fail.

## Common changes

- **Add a new code-flow fact kind** — declare its constant in a new or
  existing file here (filename must not repeat `code/`), decide with the
  owner whether it joins `FlowReadFactKinds()`, and if so update both SQL
  sites named above plus their guard tests in the same change.
- **Rename an exported kind constant** — every current caller still
  references the pre-move `facts.Code*` name through the facts root's
  `compat_code.go` forwarder (see `README.md`'s "Depended on by"); a rename
  here must update the matching `compat_code.go` entry in the same change
  so the two do not diverge.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md`, and `AGENTS.md` present.
- **`verify-dirgate.sh`** — this directory counts against the repo's
  per-directory file cap; check before adding files.
- **`verify-filename-stutter.sh`** — a new file whose name repeats `code`
  fails on Added/Renamed paths.
