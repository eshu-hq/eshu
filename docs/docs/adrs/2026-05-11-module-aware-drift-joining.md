# ADR: Module-Aware Drift Joining For Terraform Config-vs-State

**Date:** 2026-05-11
**Status:** Accepted
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- `2026-04-20-terraform-state-collector.md` — owns the state-side address
  shape `<module>.<type>.<name>` emitted by
  `go/internal/collector/terraformstate/identity.go:26`.
- `docs/superpowers/plans/2026-05-10-tfstate-config-state-drift-design.md` —
  §5 drift-kind contract this ADR keeps intact and §9 open questions this ADR
  partially closes for module-nested addresses.
- Issue: `#169`
- Prior wiring PR: `#165`. Attribute-walker PR: `#184` (intra-resource only;
  does not address cross-file module expansion).
- Prior-config walk ADR seam: `#191` (commit `25a8472`) added
  `loadPriorConfigAddresses`; this ADR adds a sibling walk for module-call
  metadata.

---

## Context

`PostgresDriftEvidenceLoader` in
`go/internal/storage/postgres/tfstate_drift_evidence.go` joins config-side
parser facts (`terraform_resources`) with state-side collector facts
(`terraform_state_resource`) by canonical Terraform resource address. The
join key today is `<type>.<name>` on both sides for the root module, which
holds when the calling repo declares every resource at the root.

It does not hold for the `module {}` pattern. A monorepo whose `main.tf`
contains:

```hcl
module "vpc" {
  source = "./modules/vpc"
}
```

produces state addresses like `module.vpc.aws_instance.web`
(`identity.go:34` prepends the state JSON `module` field). The HCL parser
runs per file and DOES walk every `.tf` in the repo snapshot — including
`./modules/vpc/main.tf` — but `configRowFromParserEntry`
(`tfstate_drift_evidence_config_row.go:29`) builds the canonical address as
`<type>.<name>` regardless of which `.tf` file the resource came from. So
the callee module's resources surface in the config map as `aws_instance.web`
while state carries `module.vpc.aws_instance.web`. The two address spaces
never meet, every module-nested resource looks state-only, and every drift
classification fires `added_in_state` even when config and state are
perfectly aligned.

The v1 limitation note in `tfstate_drift_evidence_config_row.go:17-18`
already cites this issue:

> The canonical address is the root-module form `<type>.<name>`. Module-nested
> state addresses surface as `added_in_state` in v1 — issue #169.

The Terraform-state collector already emits the canonical Terraform
address shape (`module.<name>[.module.<name>...].<type>.<name>`). The fix
must align the config side with that shape, or it must rewrite the state
side to drop the module prefix. Rewriting state is wrong: the prefix is
canonical truth from `terraform state list`, the prefix carries the
operator-meaningful module identity, and the platform already uses it
elsewhere for trace-back. Config alignment is the only correct direction.

## Decision Drivers

1. **Accuracy first.** False `added_in_state` rows are worse than no drift
   detection; operators ignore the channel after the third false positive
   and stop trusting `removed_from_state` and `attribute_drift` along with
   it.
2. **Parser stays per-file.** The HCL parser API is per-file
   (`go/internal/parser/hcl_language.go:8-17`) with no repo tree, no
   sibling-file visibility, and no filesystem walker. Touching that
   contract leaks scope into a leaf the rest of the ingester treats as
   pure.
3. **Module-call evidence is already in Postgres.** `terraform_modules`
   facts exist today (`parser.go:208`) and carry `name`, `source`, and the
   calling `path`. The data needed to derive a module prefix is already a
   committed fact under the same `(scope_id, generation_id)` the loader
   already joins on.
4. **Service boundary preservation.** Per `CLAUDE.md` §6, parser owns
   per-file language behavior and storage/postgres owns the drift loader.
   The work belongs where the data shape lives.
5. **Failure modes must be operator-actionable.** Unresolvable sources
   (registry refs, Git URLs, cross-repo) must not silently drop
   resources from the join. They must produce a deterministic fallback
   the operator can recognize and remediate.

## Considered Options

### Option A — Parser-side cross-file walk

The HCL parser encounters `module "vpc" { source = "./modules/vpc" }` and
recursively parses the callee directory. Resources discovered in the
callee are emitted with the module-prefixed address
`module.vpc.<type>.<name>`.

**Pros**

- Addresses arrive at the loader ready to join; no per-call SQL.
- Single source of address truth lives in the parser.

**Cons**

- Parser becomes filesystem-aware. Today `Parse(path, ...)` reads exactly
  one file. Option A turns it into a directory walker that has to follow
  relative paths, resolve them against the calling file's directory,
  detect cycles, and bound depth. That is a different API and a different
  ownership boundary (see `parser/hcl/AGENTS.md`).
- Parser would need to handle the same file twice: once standalone when
  walked as `./modules/vpc/main.tf` (no module prefix) and again when
  reached through the module call (with prefix). Duplicate emission would
  poison the address keyspace.
- Same callee parsed from two different module calls in the same repo
  produces two different prefixes for the same resource. The parser
  cannot know which root caller it is reached from when it reads
  `./modules/vpc/main.tf` standalone, so it cannot emit prefixed rows
  there without losing the unprefixed variant.
- A cycle in module sources (`./a` -> `./b` -> `./a`) would crash the
  parser unless every adapter learns cycle detection.
- The change must replicate inside Terragrunt and any future
  `chart {}`/`overlay {}` patterns. Each new caller pattern multiplies
  the cross-file logic.
- Two-phase parsing breaks the ingester's streaming guarantee. The parser
  currently feeds `parsed_file_data` straight through the fact queue; a
  walker would need to defer some emission until siblings are resolved.

### Option B — Loader-side module-call join (chosen)

Extend `PostgresDriftEvidenceLoader` to walk `terraform_modules` facts in
the same `(scope_id, generation_id)` it already reads
`terraform_resources` from, build a map from callee directory prefix to
module-call address chain, and prefix the config-side address when the
resource's file path matches a known callee prefix. Resources whose path
does not match any module-call callee keep the root-module address
`<type>.<name>` (unchanged behavior).

**Pros**

- Parser API stays per-file. No new boundary in `parser/hcl`.
- Module-call evidence is already in Postgres; no new fact kind.
- The loader already runs per-intent and per-snapshot, so the cost is
  bounded by one extra SQL read per intent, not per-file.
- Unresolvable module sources (`git::https://...`, registry refs, cross-
  repo) become a single classifier branch the loader can detect and label
  with the fallback `added_in_state` deterministically.
- Same-callee-multiple-callers is naturally expressible: the loader joins
  on `(callee_path, source_kind)` and emits both prefixed addresses when
  two `module {}` blocks point at the same source.
- Cycle detection lives in one place (the loader's prefix-resolution
  walker), not in every language adapter.

**Cons**

- Loader gets nontrivial. The depth of the module-call chain
  (`module.a.module.b.module.c.<type>.<name>`) must be walked at join
  time. Mitigation: bounded depth + cached prefix map per intent.
- Path normalization (relative `./` and `../`, trailing slashes, case)
  becomes load-bearing in storage code instead of parser code.
- A new SQL helper joins `terraform_modules` facts; one additional query
  per drift intent. Mitigated by the existing per-intent cache shape and
  the prior-config-walk precedent (`#191`).

### Why Option B wins

The issue body recommends Option B and the evidence backs that
recommendation. The decisive argument is that `terraform_modules` facts
already exist with the exact fields needed (`name`, `source`, `path`) and
that the parser's per-file scope is a real boundary worth preserving.
Option A's cross-file walking would have to invent ordering, cycle
detection, and prefix arbitration the loader can do in one bounded pass
over already-committed evidence. The loader is also the layer that owns
"join two committed fact streams into drift candidates"; module-aware
joining is exactly that job.

## Decision Outcome

Eshu adopts **Option B** for issue #169. The
`PostgresDriftEvidenceLoader` learns module-aware joining by walking
`terraform_modules` facts within the active commit anchor and building a
callee-directory → module-prefix map before `loadConfigByAddress` emits
its `ResourceRow` values. Resources whose `path` matches a callee
directory receive the canonical Terraform-state address shape
`module.<name>[.module.<name>...].<type>.<name>`. Resources whose `path`
does not match any callee keep `<type>.<name>` exactly as today.

### Module Source Resolution — In Scope For v1

The v1 callee resolver supports **local-filesystem source paths only**:

- `source = "./relative/path"`
- `source = "../sibling"`
- `source = "path/below/repo/root"` (treated as a repo-relative
  reference when it resolves under the calling file's repo root).

Resolution algorithm:

1. Take the calling file's `path` (committed on every
   `terraform_modules` fact at `parser.go:202`).
2. Take `Dir(path)` as the call site directory.
3. Join the call site directory with `source`, clean with `path.Clean`
   (forward-slash semantics; the path is a Postgres-stored string, not a
   live filesystem path). Implementation note: this uses Go's `path`
   package, NOT `path/filepath`. The two are different stdlib packages —
   `path` is forward-slash-only and stable across OSes, `path/filepath` is
   OS-specific and would mis-split on Windows builds. The implementation
   was tightened during PR #202 to make this explicit; an early ADR draft
   referenced `filepath.Clean`, which compiles but silently introduces an
   OS-specific regression.
4. If the cleaned result escapes the repo snapshot root (any leading
   `..` after Clean), reject the resolution and surface as a fallback
   (see below).
5. Use the cleaned directory as the callee prefix; every config-side
   `terraform_resources` row whose `path` lies under that directory
   inherits the module prefix from this call.

The resolver bounds depth at **10 nested module levels**. Beyond that, the
callee is treated as unresolvable and falls back to `added_in_state`. Ten
levels is more than any Terraform repo Eshu has seen in dogfood corpora;
the bound exists to make cycles cheap to break, not as a real ceiling.

Cycle detection is direct: the resolver tracks visited callee directories
inside one expansion and refuses to re-enter. A cycle emits a single
warning log per intent and is treated as fallback.

### Module Source Resolution — Out Of Scope For v1 (Fallback)

Sources that cannot be resolved to a directory under the same repo
snapshot are **not errors**. They are operator-actionable evidence
classes:

- `source = "terraform-aws-modules/vpc/aws"` — Terraform Registry ref.
- `source = "git::https://github.com/..."` — Git URL ref.
- `source = "https://example.com/module.zip"` — HTTP archive.
- `source = "hashicorp/consul/aws"` — short Registry form.
- Any `source` whose cleaned local resolution escapes the repo root.

For these, the loader records the module call but does not attempt to
prefix any config-side resource. State-side resources under such a module
will surface as `added_in_state` with the existing classification path.
That is correct: from Eshu's vantage point in v1 the callee module's
config is genuinely outside the joined snapshot, and the operator's
remediation is the same as a real `added_in_state` — vendor the module
locally, or wait for cross-repo support.

A dedicated `eshu_dp_drift_unresolved_module_calls_total{reason}` counter
will let operators size how much config is missed by this fallback per
intent. Reasons at launch:

- `external_registry`
- `external_git`
- `external_archive`
- `cross_repo_local`
- `cycle_detected`
- `depth_exceeded`
- `module_renamed`

### Cross-Repo Modules — Explicitly Out Of Scope

A `module {}` whose `source` points at a sibling repository in the same
Eshu snapshot is out of scope. This is not a v1 capability and is
deferred to a follow-up issue. The fallback above (`cross_repo_local`)
classifies these so the deferred work has a measurable signal to size
against.

### Address Shape — Canonical Terraform Form

The loader emits the canonical Terraform address shape exactly as state
emits it:

```
module.<name>[.module.<name>...].<type>.<name>
```

This matches `go/internal/collector/terraformstate/identity.go:26`. It
matches `terraform state list` output. It matches what an operator
copy-pastes into `terraform import` or `terraform state mv`. No
alternative shape is considered.

`count` and `for_each` index suffixes (`[index:1]`, `[key:<hash>]`) are
already produced by the state side at `identity.go:36-40`. The config
side does not produce them today (the parser emits a `count` or
`for_each` expression string, not an expanded set). That mismatch is
**out of scope** for this ADR; resources behind `count` or `for_each`
continue to surface in their existing classification mode and the
loader's module-aware prefixing applies to the unindexed address.

### Where The Code Lives

| Surface | File | What changes |
| --- | --- | --- |
| New SQL query | `tfstate_drift_evidence_sql.go` | `listModuleCallsForCommitQuery` selects `terraform_modules` rows by `(scope_id, generation_id)`. |
| Loader extension | `tfstate_drift_evidence.go` | A new helper builds the callee-directory → module-prefix map before `loadConfigByAddress` decodes resources. The existing `loadConfigByAddress` adds a single `module_prefix` lookup keyed off the row's `path`. |
| Config row builder | `tfstate_drift_evidence_config_row.go` | `configRowFromParserEntry` learns an optional `modulePrefix` parameter and prepends it when set. Doc comment loses the v1 limitation note. |
| New helper file (if needed) | `tfstate_drift_evidence_module_prefix.go` | Standalone path-resolution + cycle-detection helper if the loader file approaches the 500-line cap. |
| Test corpus | `tfstate_drift_evidence_module_prefix_test.go` | Positive, negative, and ambiguous cases. |

Parser code under `go/internal/parser/hcl/` is **not** modified. The
`terraform_modules` fact contract is unchanged.

## Edge Cases This ADR Commits To Handling

The correlation-truth gate (`CLAUDE.md` §"Correlation Truth Gates")
requires positive, negative, and ambiguous cases. The implementation
plan must produce tests for every row:

| Case | Class | Expected behavior |
| --- | --- | --- |
| Resource in `./modules/vpc/main.tf` declared, state has `module.vpc.aws_instance.web` with matching attrs | Positive | Aligned; no drift. |
| Resource in `./modules/vpc/main.tf` declared, state has `module.vpc.aws_instance.web` with different `tags` | Positive | `attribute_drift` on `tags` (the existing classifier path now fires because the addresses align). |
| Same callee `./modules/vpc` referenced by two `module {}` blocks (`module.vpc_a` and `module.vpc_b`) | Positive | Loader emits both `module.vpc_a.<type>.<name>` and `module.vpc_b.<type>.<name>` from the single callee resource row. |
| Nested module call `module.platform.module.vpc.aws_instance.web` | Positive | Multi-level prefix concatenation; resolver walks the call chain. |
| State has `module.vpc.aws_instance.web`; config has `module {}` block but `source = "terraform-aws-modules/vpc/aws"` | Negative for the new path; positive for fallback | `added_in_state`; `unresolved_module_calls_total{reason="external_registry"}` increments. |
| State has `module.vpc.aws_instance.web`; config has `module {}` block but `source = "git::https://..."` | Negative for new path; positive for fallback | `added_in_state`; counter reason `external_git`. |
| `module {}` block with `source = "../../other-repo/modules/vpc"` that escapes repo root | Negative for new path; positive for fallback | `added_in_state`; counter reason `cross_repo_local`. |
| Two `module {}` blocks form a cycle (`a -> b -> a`) | Ambiguous | Resolver detects cycle, breaks at second visit, increments counter reason `cycle_detected`, both blocks fall back to `added_in_state`. |
| `module {}` block whose `source` resolves to a directory with zero `.tf` files (empty module) | Ambiguous | No prefix entries created; no drift candidates affected. |
| `module {}` block whose `source` is a literal empty string or unparseable expression | Ambiguous | Treated as unresolvable; counter reason `external_archive` (catch-all for non-local non-VCS). The parser already trims this at emission, so the loader should see an empty `source` and skip. |
| Resource declared at the **repo root** (no `module {}` involvement) | Negative (baseline) | Address stays `<type>.<name>` exactly as today. Regression guard: existing root-module tests must continue to pass byte-identically. |
| State address contains `[index:N]` or `[key:<hash>]`, config side has matching unindexed declaration | Ambiguous | Out of scope for this ADR; existing behavior unchanged. |
| `terraform_modules` fact missing entirely for a repo that uses module blocks (parser bug, fact deletion) | Ambiguous | Loader degrades gracefully — no prefix map, every nested resource falls back to `added_in_state`. This is also the fallback for "repo has no modules at all," which is the common case. |
| Prior-config walk (`#191`) interacts with module-prefixed addresses | Positive | `loadPriorConfigAddresses` must apply the module-prefix map for each prior generation; otherwise `removed_from_config` would lose its module-nested half. |
| Module block renamed across generations (`module.vpc` -> `module.network`) while the callee path persists | Ambiguous | Current config projects to the current module name, but prior-config projection uses the prior generation's module name so the old state address is still marked `PreviouslyDeclaredInConfig`. Counter reason `module_renamed` increments once per prior generation and callee path. |
| Same address resolves via two different callees with conflicting attributes (one callee says `tags.env=prod`, another says `tags.env=stage`) | Ambiguous | Each prefixed address is its own keyspace entry; no merge happens. State-side has the prefix that distinguishes them. |

## Consequences

### Positive

- Drift detection on `module {}` monorepos becomes accurate. Today's
  `added_in_state` flood drops to the residual real-import set.
- `attribute_drift`, `removed_from_state`, and `removed_from_config`
  classifications start producing useful signal on the same repos.
- Eshu publishes a measurable `unresolved_module_calls_total` counter so
  operators can size the gap to cross-repo and registry support.
- The fix is bounded to one package boundary (`storage/postgres`) and
  does not touch parser, reducer, collector, or graph code.

### Negative

- `PostgresDriftEvidenceLoader` grows a new responsibility (callee-
  directory resolution) that today is intuitively a parser concern.
  Mitigation: the helper is one file with one entry point and a clear
  contract; doc comments cite this ADR.
- An additional SQL read per drift intent. Bounded by intent count and
  by the size of `terraform_modules` rows, not by repo scale. The
  prior-config-walk precedent (`#191`) operates at the same shape.
- Registry-sourced modules continue to surface as `added_in_state`. The
  operator-facing remediation is "vendor the module locally" or "wait
  for cross-repo support" — same advice the v1 limitation already
  produced, but now with a deterministic counter.

### Risks

- **Path normalization mistakes.** Forward-slash vs OS-specific
  separators, trailing slashes, or `..` semantics could mis-bucket
  resources. Mitigated by `path.Clean` on forward-slash inputs and
  by tests that exercise every separator/cleanup variant.
- **Callee-directory inheritance ambiguity.** A `.tf` file inside
  `./modules/vpc/submodule/main.tf` could be reachable through both
  `module "vpc"` (root caller) and an inner `module {}` call inside
  `./modules/vpc/main.tf`. The resolver must walk the inner call too,
  not flatten on the outermost only. The test matrix above covers this
  through the nested-module case.
- **Prior-config interaction.** `loadPriorConfigAddresses` must apply a
  module-prefix map built from each prior generation before projecting
  that generation's resources; otherwise `removed_from_config` regresses
  on module-nested addresses and module renames create false pairs. The
  implementation includes regression tests for the preserved-module and
  renamed-module shapes.
- **Performance.** A pathological repo with hundreds of `module {}`
  blocks and thousands of `.tf` files would build a large prefix map.
  Bounded by repo-scale fact counts that already drive
  `listConfigResourcesForCommitQuery`; if the prefix-build cost shows
  up in traces, the implementation plan adds a histogram and a
  per-intent cache.

## Open Questions Requiring Sign-Off

These must be resolved before the implementation plan can be written.

1. **Should the new fallback counter
   `eshu_dp_drift_unresolved_module_calls_total{reason}` be one counter
   with seven reasons, or independent counters?** This ADR proposes
   one counter with a reason label, matching the existing
   `eshu_dp_correlation_drift_detected_total{drift_kind}` shape.
   Operator workflows already filter on `reason`/`drift_kind` labels.
2. **Should fallback behavior also emit a `terraform_drift_warning`-
   style fact for operator visibility outside metrics, or is the
   counter + structured log sufficient?** Recommendation: counter +
   structured log for v1; promote to a warning fact only if operator
   feedback shows the metric is not discoverable in dashboards.
3. **Should the module-prefix map be cached across intents within one
   reducer pass?** Recommendation: not in v1. The SQL is cheap and
   per-intent. Cache lifetime concerns (eviction on commit anchor
   change) outweigh the read savings until measurement proves
   otherwise.
4. **Do we want the implementation plan to also fix the `count` /
   `for_each` index-suffix gap?** Recommendation: no. Issue #169 is
   already the biggest item in the queue; index-suffix expansion is a
   separate evidence problem (the parser doesn't know expansion keys
   without evaluating expressions). File as a follow-up.
5. **Should `removed_from_config` activation tests for module-nested
   addresses live in this issue's PR or in a follow-up?** This ADR
   asserts they must land in the same PR because skipping the
   prior-config map application would silently regress `#191`.
   Confirm.

## References

- HashiCorp Terraform module sources reference (local paths, registry,
  Git, archive, S3 forms):
  <https://developer.hashicorp.com/terraform/language/modules/sources>
- HashiCorp Terraform resource addressing (canonical
  `module.<name>...<type>.<name>` shape used by `terraform state list`,
  `terraform import`, `terraform state mv`):
  <https://developer.hashicorp.com/terraform/cli/state/resource-addressing>
- Issue #169 — original scope and option discussion.
- PR #165 — wiring PR that planted the v1 limitation note this ADR
  removes.
- PR #184 — intra-resource attribute walker; deliberately not the same
  problem.
- PR #191 — prior-config walk precedent for "extra SQL read per intent
  in the drift loader."
