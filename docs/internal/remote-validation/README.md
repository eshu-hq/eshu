# docs/internal/remote-validation

Evidence artifacts for `remote_validation` proof-IDs cited in
`specs/capability-matrix.v1.yaml` and `specs/capability-matrix/*.yaml`
(#5407, PR 2 of #5336).

## Convention

A matrix row's `production` profile may cite a `remote_validation` proof, for
example:

```yaml
production: {status: supported, verification: [{remote_validation: prod-code-search-exact}]}
```

That `prod-code-search-exact` ref must resolve to a committed file at:

```
docs/internal/remote-validation/prod-code-search-exact.md
```

`go/internal/capabilitycatalog/remote_validation.go`
(`CheckRemoteValidationArtifacts`) enforces this contract, run by
`scripts/verify-remote-validation-artifacts.sh` (CI gate
`remote-validation-artifacts` in `specs/ci-gates.v1.yaml`). A ref with no valid
deployed-tier evidence here fails unless it is listed in the burn-down baseline,
`specs/remote-validation-baseline.txt`. A placeholder file does not clear debt.

A ref is validated against the slug shape `^[a-z0-9]+(-[a-z0-9]+)*$` **before**
it is ever joined into a path or probed with `os.Stat`, and the resolved path is
re-checked to stay under this directory. A ref that is not a valid slug (for
example a `../../etc/passwd` path-traversal payload) can neither escape this
directory nor be excused by a baseline entry: it is always a hard finding naming
the ref and its capability/profile.

The baseline is a **frozen audited set**, not a soft "known debt" list. Every
slug in it is a capability-matrix row whose `production` profile claims
`{status: supported}` and whose sole verification evidence is a
`remote_validation` ref that resolves to no committed artifact — this directory
did not exist when the gate was introduced, so each is a top-tier
production-support claim resting on zero committed proof. Freezing the set stops
the debt from growing; it does not cure the claims. The systemic per-row
validate-or-downgrade of every baselined slug is tracked in **#5552, which
blocks epic #5344 closure**.

The baseline carries a `# FROZEN_MAX: <N>` directive that acts as a ratcheting
high-water mark. The gate fails when the entry count **exceeds** the ceiling, so
a new unverified `production:supported` row cannot be smuggled in by appending
its ref and running `-update`. Burning down a slug and running `-update` lowers
`FROZEN_MAX` to the new, smaller count; `-update` never raises it. Raising the
ceiling requires an explicit, separately-reviewed one-line edit.

The `FROZEN_MAX` ceiling alone does **not** stop a constant-count *atomic swap*:
in a single edit an attacker could burn down one legitimately baselined ref
(committing its artifact, so it leaves the baseline) while adding a new unbacked
claim, keeping the entry count at the ceiling. To close that, the baseline is
bounded by an **immutable frozen membership set**,
`specs/remote-validation-frozen.txt` — the audited-at-introduction set of the
115 slugs. The gate enforces `baseline ⊆ frozen`: a ref may be baselined only if
it is also in the frozen set, so a new claim (absent from the frozen set) is
rejected even at constant count. The frozen file loads **fail-closed** (a
missing or malformed file fails the gate). `-update` **never** writes the frozen
set; it only regenerates the baseline (the dangling subset of the frozen slugs).
A slug leaves the frozen set only when its row is validated-or-downgraded and
removed from **both** files in the same reviewed edit.

## Writing an artifact

An evidence file records what actually ran against deployed services. It must
include these machine-checked fields, each on one line:

```text
Validation-Slug: prod-example
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/run-remote-e2e-example.sh
Validation-Command: bash scripts/run-remote-e2e-example.sh; echo $?
Validation-Exit-Code: 0
Capability-Assertion: capability.id returns a non-empty exact result through the deployed API.
B12-Assertion: capability.id -> mcp:list_capability_results
```

`Evidence-Kind` is `compose_e2e` for a committed Compose driver,
`deployed_e2e` for a committed Kubernetes, hosted, or remote-e2e driver, or
`live_backend` for a committed `docs/internal/evidence/*.md` live-backend
record. `Evidence-Source` must exist and match that kind. The command must end
with direct `; echo $?` capture, and the captured exit must be `0`. Add one
`Capability-Assertion` per capability when several production rows share a
slug. Add a matching `B12-Assertion` for each capability, using the exact
`<transport>:<query-shape-key>` from the committed B-12 snapshot. The verifier
rejects missing or unknown B-12 pointers, so every production claim resolves to
the concrete deployed assertion that exercised it. A local `go_test` run is
useful lower-tier evidence, but it cannot retain a `production: supported`
claim.

Keep credentials, hostnames, account IDs, IPs, key paths, and local machine
paths out of the artifact. Once the evidence is valid, remove the ref from
`specs/remote-validation-baseline.txt` (or run
`bash scripts/verify-remote-validation-artifacts.sh -update`, which drops it
automatically and ratchets `FROZEN_MAX` down). Removing the ref
from `specs/remote-validation-frozen.txt` as well is optional but keeps the two
files aligned; `-update` does not touch the frozen file.

The matrix is the source of truth for expected slugs and rows. Regenerate the
compact checked-in index without touching evidence measurements:

```bash
bash scripts/generate-remote-validation-inventory.sh
```

The generator writes
`docs/internal/remote-validation/inventory.generated.json`. Schema version 2
includes `assertion_count` for each artifact; tests cross-check that count
against both assertion fields in the artifact. The generator never writes a
validation date, command result, or capability assertion.

## Current state

At freeze time (#5407) every `remote_validation` ref cited in the matrix (115
unique slugs across 120 row-occurrences) predated this gate and had no committed
evidence file. All 115 were frozen in `specs/remote-validation-frozen.txt` (the
immutable audited-at-introduction set) and also carried in
`specs/remote-validation-baseline.txt` under `FROZEN_MAX: 115`. Closing an entry
requires either committing a real artifact (the capability keeps its claimed
status) or an explicit, separately-reviewed decision to downgrade the
capability's claimed status. The systemic burn-down of all 115 is tracked in
#5552, which blocks epic #5344 closure.

Burn-down progress:

- **#5666** downgraded the pair #5336 originally flagged —
  `prod-component-extension-inventory` / `prod-component-extension-diagnostics`
  (`component_extensions.inventory` / `.diagnostics`) — from `production:
  supported` to `experimental`, because no committed evidence exercised the
  read surface through the API/MCP. **That gap has since been closed and both
  rows are back at `production: supported`**: the two artifacts in this
  directory record a live `GET /api/v0/component-extensions` readback against a
  deployed component-extension Compose stack, returning `installed=true`,
  `enabled=true`, and `trusted=true` for a real claimed component, with the
  diagnostics route sharing that handler.
- **This directory now holds 110 committed, machine-valid deployed artifacts**
  covering all 115 currently supported production row-occurrences. Seven were
  validated for #5681. #5552 requalified the remaining 103 slugs: 100 through
  a fresh 547-pass golden-corpus Compose run, two through the deployed
  component-extension driver, and one through the dedicated dead-IaC Compose
  driver. Every artifact records its matching deployed tier, run date, exact
  command with direct exit capture, evidence source, and one assertion per
  production capability. No row was downgraded in the #5552 tranche.
- **The cluster of 7 code-intelligence slugs before this one**
  (#5681) was resolved per-row against a real deployed-services stack: the two
  transitive-caller-graph reads (`prod-transitive-callers` /
  `prod-transitive-callees`) returned complete multi-hop
  `authoritative_graph` results and keep `production: supported` with a
  committed deployed-readback artifact here (their declared p95 is a separate
  performance budget tracked by the perf gates, not a per-capability support
  blocker). The other five — `symbol_graph.imports`, `code_flow.reaching_def`,
  `semantic_evidence.code_hints.list`, `symbol_graph.inheritance`, and
  `symbol_graph.argument_names` — were **downgraded to `experimental`** because
  their declared deployed route returned empty or incomplete results, each
  citing the product defect that must land before the claim is restored
  (imports has no producer #5691; reaching_def's wiring landed under #5692 but
  no deployed run has been captured with the gate on; code_hints has a producer
  under #5693 that no runtime path calls yet; inheritance hits the NornicDB
  `type(rel)`/`coalesce`-after-`OPTIONAL MATCH` literal-text defect #5694; and
  argument_names drops declared parameters through projection). No slug was
  bulk-downgraded: every one carries a per-row deployed determination.
- **`FROZEN_MAX` is now 0 — the baseline is empty.** The final 7
  secrets/IAM and code-to-cloud/code-search slugs closed Cluster B of #5681:
  `prod-secrets-iam-identity-trust-chains`, `-posture-gaps`,
  `-posture-summary`, `-privilege-posture-observations`,
  `-secret-access-paths`, `prod-trace-exposure-path`, and
  `prod-variable-lookup`. Each was **validated** on a fresh, uniquely named
  deployed-services Compose stack and keeps `production: supported`. The five
  `secrets_iam.*` reads returned non-empty rows or non-zero summary buckets from
  synthetic Kubernetes, AWS, and Vault evidence; they did not require the
  optional graph-projection flag. `trace_exposure_path` resolved the source and
  returned the promised bounded unresolved state rather than inventing a path.
  MCP `find_code` and HTTP code search both returned Variable-labeled matches.
  The same edit corrected four local-profile evidence mismatches. A second
  fresh golden-corpus run under `ESHU_QUERY_PROFILE=local_authoritative`
  exercised `code_search.variable_lookup` and
  `code_to_cloud.trace_exposure_path` against the real graph stack, so those
  local_authoritative rows remain `supported`. Their local_full_stack rows cite
  the same credential-free Compose driver and also remain `supported`. See
  TRANCHE 2 in [DISPOSITIONS.md](DISPOSITIONS.md) and the seven dated artifacts
  in this directory carry the exact command, direct exit capture, and per-row
  observed results.
  Closing this pointer baseline to empty resolves Cluster B and allows #5681 to
  close. It does **not** close #5552: that program stays open until all 103
  legacy files pass matching-tier content validation and the recurrence gate
  proves seeded unbacked claims red and the reverted matrix green.

The #5552 tranche now satisfies that remaining condition: the strict real-tree
verifier accepts all 110 supported slugs, and its hermetic BITES test seeds an
unbacked production row RED before reverting the matrix to GREEN. The generated
per-row index is [inventory.generated.json](inventory.generated.json), and the
human disposition record is [DISPOSITIONS.md](DISPOSITIONS.md).
