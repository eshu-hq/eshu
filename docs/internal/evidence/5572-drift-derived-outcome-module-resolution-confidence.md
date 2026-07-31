# Evidence: #5572 per-address module-resolution-confidence signal ("derived" outcome)

This note carries the performance/no-regression and observability evidence
markers `scripts/verify-performance-evidence.sh` requires for the hot-path
files this change touches.

Touched hot-path files this covers:

- `go/internal/storage/postgres/tfstate_drift_evidence.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_config_row.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_module_prefix.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_module_confidence.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_prior_config.go`
- `go/internal/storage/postgres/aws_cloud_runtime_drift_evidence.go`
- `go/internal/storage/postgres/terraform_config_state_drift_findings.go`
- `go/internal/reducer/terraform_config_state_drift_writer.go`

## What changed and why it does not add a new query, join, or scan pattern

`buildModulePrefixMap` already read every `terraform_modules` fact for the
generation and already walked the module-call graph once per generation. This
change adds zero new Postgres queries: it populates a second, in-memory
`moduleResolutionConfidenceMap` (a plain `map[string]string`) alongside the
existing `modulePrefixMap` from data the SAME query result set already
produced, and records into it either (a) once per `external_registry`-
classified call (a single extra `resolveLocalCallee` string join+clean, the
same O(1) operation `classifyModuleSource` already performs for every call)
or (b) once per `depth_exceeded` event (already a rare, bounded-depth event —
`maxModulePrefixDepth` caps the recursion at 10).

Per `terraform_resources` entry, `emitConfigRowsForEntry` now does ONE
additional directory-walk-up lookup
(`moduleResolutionConfidenceMap.reasonForPath`), which is the exact same
walk-up-the-directory-chain algorithm `modulePrefixMap.modulePrefixForPath`
already performs for every entry — same O(directory depth) shape, doubling
the constant factor of an already-bounded (`maxModulePrefixDepth` = 10)
per-entry walk. No loop was changed from O(1) to O(n) or worse; no new
per-entry Postgres round trip was added; the four SQL queries
`LoadDriftEvidence` issues are unchanged in count, shape, and predicates.

- No-Regression Evidence (#5572): `cd go && go test
  ./internal/correlation/drift/tfconfigstate/...
  ./internal/storage/postgres/... ./internal/reducer/... ./internal/query/...
  ./internal/mcp/... -count=1` is green, including the full pre-existing
  module-aware-joining suite (`TestLoadConfigByAddressAppliesModulePrefixForCalleeFiles`,
  `TestLoadConfigByAddressExpandsSameCalleeForMultipleCallers`,
  `TestLoadConfigByAddressNestedChainProducesMultiLevelAddress`,
  `TestLoadConfigByAddressRootModuleResourcesKeepIdenticalAddress`,
  `TestLoadPriorConfigAddressesAppliesModulePrefix`,
  `TestLoadPriorConfigAddressesUsesPriorGenerationModulePrefixOnRename`) with
  byte-identical addresses and prefix counts to before this change --
  proving the new confidence bookkeeping is purely additive and does not
  alter any existing address, join, or drift classification. The complexity
  argument above (same query count, same per-entry walk shape, bounded event
  counts) is the proof; there is no separate query-plan or throughput claim
  to benchmark because no query, index, or scan changed.
- Observability Evidence (#5572): no new metric or span. The change reuses
  the existing `eshu_dp_drift_unresolved_module_calls_total{reason}` counter
  path unchanged (issue #169) and adds a per-finding
  `terraform_module_resolution_confidence` evidence atom that flows through
  the SAME `Evidence []map[string]any` field the drift finding's payload
  already serializes and the SAME
  `POST /api/v0/terraform/config-state-drift/findings` response the finding
  row already returns -- no new response field, no new log line, no new
  telemetry instrument. `TestPostgresTerraformConfigStateDriftWriterDowngradesOutcomeToDerivedWhenModuleResolutionReasonPresent`
  proves the evidence atom and the downgraded `outcome="derived"` both reach
  the durable row.

## Golden-corpus coverage per cause (review follow-up)

Issue #5572's two documented causes get DIFFERENT proof tiers, a deliberate,
stated decision rather than a silent gap:

- **`external_registry` has live golden-corpus coverage.**
  `tests/fixtures/ecosystems/terraform_comprehensive/terraform-aws-modules/vpc/aws/main.tf`
  is a real directory sitting at the EXACT path `modules.tf`'s pre-existing
  `module "vpc" { source = "terraform-aws-modules/vpc/aws" }` block resolves
  to as a local relative path -- the ADR's own documented false-positive
  shape, now made real instead of theoretical (that source was previously a
  dead reference; no directory existed there). It declares one genuine
  resource (`aws_security_group.vpc_endpoints`). The matching cassette side
  (`testdata/cassettes/terraformstate/supply-chain-demo.json`) carries the
  CORRECT module-prefixed address
  (`module.vpc.aws_security_group.vpc_endpoints`) as a separate
  `terraform_state_resource` fact under the same pre-existing S3-backed
  scope, so neither side's address matches the other -- the real, live
  spurious `added_in_config`/`added_in_state` pair the ADR describes, not a
  synthetic single-sided fixture. The
  `POST /api/v0/terraform/config-state-drift/findings?variant=derived`
  entry in `testdata/golden/e2e-20repo-snapshot.json`
  (`minimum_results: 2`, raised from the original `1` when review flagged
  that a floor of `1` cannot distinguish "both halves downgraded" from
  "only the config-side half downgraded, as before this section's own
  follow-up fix") asserts, via TWO `required_json_object_matches` entries
  under `drift_findings[]`, that a finding with `outcome="derived"` exists
  at address `aws_security_group.vpc_endpoints` (the config-only half) AND
  that a SEPARATE finding with `outcome="derived"` exists at address
  `module.vpc.aws_security_group.vpc_endpoints` (the state-only half) --
  since one finding object cannot carry two different `address` values at
  once, this pins two distinct derived findings, not merely a count a
  regression retaining only the config-side downgrade could also satisfy.
  A third `required_json_object_matches` entry under
  `drift_findings[].evidence[]` (via `required_json_object_matches`, not
  independent wildcard value checks) proves a finding's evidence array
  carries a `terraform_module_resolution_confidence` atom with
  `value="external_registry"` on one correlated object -- proving the
  specific cause reaches the read surface, which is the entire
  justification for keeping one `derived` outcome value instead of
  splitting per cause (see `tfconfigstate/doc.go`'s "Outcome model"
  section). This mirrors issue #5594's precedent in this same writer: a
  unit-tested behavior change to reducer-materialized, OpenAPI- and
  MCP-contracted truth gets cassette/golden replay proof, not only fakes.
  The `2` floor was derived, not assumed, by tracing the actual fixture and
  cassette content: `buildModulePrefixMap` flags exactly one directory for
  this repo's single ingested generation (`module.s3_bucket`'s
  `./modules/s3` is a clean local path with no leading-registry-shorthand
  match; `module.eks`'s `git::` source is `external_git`, never flagged by
  `recordRegistryHeuristicCandidate`), that directory declares exactly one
  resource, and the cassette's four `terraform_state_resource` facts for
  this scope contain exactly one address sharing that resource's
  `<type>.<name>` key -- an unambiguous 1:1 pairing. This repo has a single
  ingested generation in the corpus, so `loadPriorConfigAddresses` finds no
  prior generation to promote a third (`removed_from_config`) `derived`
  finding through.
- **`depth_exceeded` deliberately stays unit/integration-only.** Reaching
  it requires an 11-level-deep local module chain
  (`maxModulePrefixDepth` = 10) -- a fixture heavy enough for a rare
  production shape (a real repo would need ten nested nameless wrapper
  modules purely to trigger this path) that
  `TestBuildModulePrefixMapRecordsDepthExceededAsLowConfidence` and
  `TestLoadDriftEvidenceMarksLowConfidenceForDepthExceededModuleChain`
  (`go/internal/storage/postgres/tfstate_drift_evidence_module_confidence_test.go`)
  already prove precisely, including the depth-comparison fix itself (the
  masking case where the resource is silently misattributed to the
  ancestor's real-but-wrong prefix rather than falling back to root). This
  is a scoping decision, not an oversight: if a future real-world report
  shows `depth_exceeded` firing in practice, a golden fixture is the
  concrete follow-up, mirroring the `external_registry` fixture added here.

## Adding a corpus resource shifts the IaC inventory count assertion (review follow-up)

A live golden-corpus-gate run confirmed the `derived`-outcome fixture above
proves the cause end to end (the `required_json_object_matches` check on
`drift_findings[].evidence[]` passed, meaning `evidence_type` and `value`
landed on the same evidence atom) -- but it also failed a SIBLING
assertion: `GET /api/v0/iac/resources?limit=50&include_facets=true`
expected `count`/`summary.by_kind.resource`/`summary.total` at their
pre-fixture values.

**This coupling is structural, not incidental, and the next person adding a
corpus fixture should expect it too.** `tests/fixtures/ecosystems/terraform_comprehensive/terraform-aws-modules/vpc/aws/main.tf`
declares one genuine `resource` block. `GET /api/v0/iac/resources`'s
`count` field is `len(results)` (`go/internal/query/iac_resources.go`) --
the returned PAGE, bounded by `limit`, not an unconditional corpus total --
but this specific golden query shape requests `limit=50` against a corpus
whose matching resource-kind population (13, pre-fixture) sits well under
that limit, so every matching resource is returned and `count` equals the
true total for this one request. `summary.by_kind.resource` and
`summary.total` are separate, unbounded aggregates
(`go/internal/query/iac_inventory_postgres.go`'s `Summary`) that scan the
same `fact_kind='content_entity'`, `TerraformResource`-labeled population
regardless of page limit. Any new `resource` block landing in an
already-onboarded fixture repo (`terraform_comprehensive` here) raises all
three by exactly the block count added, with `module`/`data` blocks moving
the same three counts through a different dimension. Issue #5594's
`terraform_local_backend_demo` fixture hit this identical coupling first
(two resource blocks, +2/+2); this fixture repeats the pattern at +1
(one resource block): `count`/`summary.by_kind.resource` 13 -> 14,
`summary.total` 21 -> 22. Both `testdata/golden/e2e-20repo-snapshot.json`'s
`required_json_values` on that query shape AND the static regression lock-in
at `go/cmd/golden-corpus-gate/snapshot_iac_inventory_test.go` needed the
matching update -- the snapshot alone is not enough; the Go test hardcodes
the same three numbers as its own regression guard against silent snapshot
drift, and `go test ./cmd/golden-corpus-gate/... -count=1` catches a
snapshot/test mismatch immediately, without needing the live gate.

Before adding a `resource`/`module`/`data` block to ANY already-onboarded
fixture repo in the corpus, check
`GET /api/v0/iac/resources?limit=50&include_facets=true`'s
`required_json_values` and its lock-in test for the count this block
type will move, and update both together in the same change.

## Follow-up: both halves of a spurious mismatch pair now downgrade

The original change above only flagged the CONFIG-side half of a spurious
`added_in_config`/`added_in_state` mismatch pair as low-confidence. An
unresolved module-prefix chain does not make one address uncertain; it makes
the config/state JOIN KEY wrong, so `mergeDriftRows` always emitted TWO
candidates for the SAME real resource -- a config-only `added_in_config` at
the fallback address (correctly flagged and downgraded to `derived`) and a
state-only `added_in_state` at the real, prefixed address (left at
`exact`, since `ResourceRow.ModuleResolutionReason` was documented as
"CONFIG-side only"). A caller filtering `outcome=exact` still got back half
of a pair this feature exists specifically to flag as uncertain. A second,
related gap: `PostgresDriftEvidenceLoader.loadPriorConfigAddresses` computed
its own `moduleResolutionConfidenceMap` per prior generation
(`priorPrefixMap, _, err := l.buildModulePrefixMap(...)`) and discarded it,
so a `removed_from_config` finding promoted from a low-confidence
prior-config address also stayed `exact`.

Touched hot-path files this follow-up covers (additive to the list above):

- `go/internal/storage/postgres/tfstate_drift_evidence_pairing.go` (new)
- `go/internal/storage/postgres/tfstate_drift_evidence_helpers.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_prior_config.go`
- `go/internal/correlation/drift/tfconfigstate/candidate.go`

### Fix shape

1. `pairSpuriousModuleMismatches` (new,
   `tfstate_drift_evidence_pairing.go`) runs inside `mergeDriftRows` before
   the per-address loop. It mirrors `ModuleResolutionReason` from a
   config-only row onto its paired state-only row ONLY when the pairing is
   unambiguous: exactly one low-confidence config-only row and exactly one
   state-only row share the same `resourceAddressKey` (see the "Follow-up:
   front-stripping fix" section below for that function's exact contract --
   an earlier last-two-segments shape shipped with this pairing was itself
   defective and was fixed before this branch merged). The ambiguous case
   (2+ candidates share a key on either side) is left untouched on purpose:
   Terraform's own idiomatic "singleton resource" naming convention
   (`aws_s3_bucket.this`, `aws_iam_role.this`, and similar, the exact
   convention `terraform-aws-modules` itself uses) means the same
   `<type>.<name>` key legitimately recurs across unrelated, independently
   resolved modules, so a blind match risks mirroring the reason onto a
   genuinely unrelated resource. Refusing ambiguous collisions accepts a
   narrower, explicitly-scoped miss (ambiguous-collision repos keep today's
   partial behavior) over any risk of a false "derived" downgrade on a real,
   independent finding.
2. `collectPriorConfigAddresses` (`tfstate_drift_evidence_prior_config.go`)
   now threads the PRIOR generation's own `moduleResolutionConfidenceMap`
   through the same `moduleResolutionReasonForEntry` match-specificity
   comparison `emitConfigRowsForEntry` already uses for the current
   generation, and `loadPriorConfigAddresses` returns `map[string]string`
   (address -> reason, `""` when clean) instead of `map[string]struct{}`.
   `mergeDriftRows` sets `ResourceRow.ModuleResolutionReason` alongside
   `PreviouslyDeclaredInConfig` whenever the promoted address carries a
   non-empty reason.
3. `BuildCandidates` (`candidate.go`) gained a `row.State != nil &&
   row.State.ModuleResolutionReason != ""` branch symmetric with the
   pre-existing `row.Config` branch, so both code paths above reach the
   read surface through the same, already-load-bearing
   `EvidenceTypeModuleResolutionConfidence` atom and the reducer writer's
   existing `moduleResolutionOutcome` (which only checks atom presence, not
   which side it came from -- no writer change was needed).

### Why this adds no new query, join, or scan pattern

`pairSpuriousModuleMismatches` operates entirely on the `config`/`state`
in-memory maps `mergeDriftRows` already receives -- zero new Postgres
queries. Its cost is two linear passes building small per-key buckets (one
entry per row that is config-only-with-a-reason or state-only, respectively,
both subsets of a join that is already bounded by one state-snapshot scope's
resource count) plus a linear pass over the resulting key buckets; no
per-entry Postgres round trip, no change to any existing query's count,
shape, or predicates. `collectPriorConfigAddresses`'s added work is one
`moduleResolutionReasonForEntry` call per prior-config entry -- the exact
same O(directory depth), already-bounded (`maxModulePrefixDepth` = 10)
walk-up-the-directory-chain lookup `emitConfigRowsForEntry` already performs
per current-generation entry, just applied to the prior-generation entries
that were already being walked.

- No-Regression Evidence (#5572 follow-up): `cd go && go test
  ./internal/correlation/drift/tfconfigstate/...
  ./internal/storage/postgres/... ./internal/reducer/... ./internal/query/...
  ./internal/mcp/... -count=1` is green, including every pre-existing
  prior-config and module-confidence test
  (`TestPostgresDriftEvidenceLoaderPriorConfigDeclarationActivatesRemovedFromConfig`,
  `TestPostgresDriftEvidenceLoaderPriorConfigNeverDeclaredLeavesFlagFalse`,
  `TestPostgresDriftEvidenceLoaderPriorConfigOutsideDepthWindowLeavesFlagFalse`,
  `TestLoadPriorConfigAddressesAppliesModulePrefix`,
  `TestLoadPriorConfigAddressesUsesPriorGenerationModulePrefixOnRename`,
  `TestLoadDriftEvidenceMarksLowConfidenceForRegistryHeuristicMisclassifiedLocalModule`,
  `TestLoadDriftEvidenceMarksLowConfidenceForDepthExceededModuleChain`) with
  byte-identical addresses, prefixes, and `PreviouslyDeclaredInConfig`
  outcomes to before this change. New regression coverage proves the fix
  itself: `TestPairSpuriousModuleMismatchesMirrorsReasonOntoUnambiguousStateOnlyRow`,
  `TestPairSpuriousModuleMismatchesSkipsAmbiguousResourceKeyCollision`,
  `TestPairSpuriousModuleMismatchesIgnoresCleanConfigRows`,
  `TestLoadDriftEvidencePairsSpuriousMismatchAcrossModuleResolutionFailure`,
  `TestPostgresDriftEvidenceLoaderPriorConfigConfidenceThreadedOntoRemovedFromConfigRow`,
  and `TestBuildCandidatesAttachesModuleResolutionConfidenceAtomWhenStateRowFlagsIt`.
- Observability Evidence (#5572 follow-up): no new metric or span. The state-
  side atom reuses the exact same `EvidenceTypeModuleResolutionConfidence`
  atom shape, evidence-array field, and API/MCP response path the
  config-side atom already used; the reducer writer's
  `moduleResolutionOutcome` is unchanged (it already checked atom presence
  generically, not which side attached it).

### Follow-up: front-stripping fix for resourceAddressKey (dotted index / data-source collisions)

Review found a real defect in `resourceAddressKey`'s FIRST shape (the one
that shipped in the commit adding `tfstate_drift_evidence_pairing.go`):
`strings.Split(address, ".")`, keep the last two segments. Proved
empirically by extracting the function and running it against realistic
addresses:

```
aws_route53_record.this["api.example.com"]            -> "example.com\"]"
module.dns.aws_route53_record.this["api.example.com"] -> "example.com\"]"
aws_acm_certificate.cert["www.example.com"]            -> "example.com\"]"   <-- collision
data.aws_ami.ubuntu                                    -> "aws_ami.ubuntu"
aws_ami.ubuntu                                         -> "aws_ami.ubuntu"   <-- collision
```

Two distinct false-pairing paths, both defeating the "unambiguous 1:1"
guard `pairSpuriousModuleMismatches` relies on for safety: a `for_each` key
containing a literal `.` collapses two UNRELATED resources
(`aws_route53_record.this[...]` and `aws_acm_certificate.cert[...]`) to the
identical wrong key, and a `data.` source collapses onto a managed resource
of the same type/name. Either one, if it happened to be the ONLY collision
in a join, would satisfy the "exactly one on each side" ambiguity check and
mirror a `ModuleResolutionReason` onto a real, unrelated finding -- a false
`derived` downgrade of true drift, the precise failure mode the ambiguity
guard exists to prevent.

**Fix: front-stripping, not end-taking.** `resourceAddressKey` now walks the
address from the LEFT, consuming and discarding one `module.<name>[<index>]`
segment at a time (`skipModuleNameSegment`), tracking bracket depth and
double-quote state so a `.` or `]` inside a quoted index key -- on either a
for_each instance's own index, or an indexed MODULE NAME's index
(`module.vpc["a.b"].aws_x.y`) -- is never mistaken for a segment boundary.
Whatever remains after stripping every leading `module.` segment is returned
verbatim (`hasResourceTypeNameShape` only validates the shape; it does not
re-split or transform). This is provably safe by construction: front-strip
mechanically removes exactly the module-prefix bytes prepended by
`configRowFromParserEntry`/`resourceAddress`, and returns everything after
that untouched.

**`data.` prefix decision, made deliberately and documented in
`resourceAddressKey`'s doc comment:** preserve it, never strip it. Terraform
itself treats `data.TYPE.NAME` and `TYPE.NAME` as different resources.
Checked whether a `data.`-prefixed address can actually reach either side of
`pairSpuriousModuleMismatches`'s maps, rather than assuming:

- Config-only rows can NEVER carry a `data.` prefix. The HCL parser routes
  `data` blocks into a separate `terraform_data_sources` bucket
  (`internal/parser/hcl/parser.go`'s `case "data":` branch,
  `shared.AppendBucket(payload, "terraform_data_sources", row)`), and
  `PostgresDriftEvidenceLoader`'s config-side query only ever reads
  `parsed_file_data->'terraform_resources'`
  (`tfstate_drift_evidence_sql.go`'s `listConfigResourcesForCommitQuery` /
  `listPriorConfigAddressesQuery`) -- `terraform_data_sources` is never
  queried by this domain at all.
- State-only rows CAN carry a `data.` prefix. The collector's
  `resourceAddress` (`internal/collector/terraformstate/identity.go:29-45`)
  explicitly prefixes `"data."` when `resource.Mode == "data"`, and
  `validateResourceIdentity` accepts both `"managed"` and `"data"` as valid
  modes -- the state-side SQL query
  (`listStateResourcesForGenerationQuery`) has no mode filter, so a
  `data.`-prefixed `terraform_state_resource` fact reaches `stateByAddress`
  unfiltered.

So the collision can only threaten the state side, and preserving the
`data.` prefix (which falls out naturally from front-stripping -- no special
case needed) closes it.

**A related finding, not a defect, worth recording:** the coordinator's
proof table used Terraform CLI-display-format addresses
(`type.name["literal-key"]`, `type.name[0]`). Checked this collector's ACTUAL
state-side address synthesis and found it does NOT emit that literal shape
at all for `for_each`/`count` instances: `resourceAddress` appends
`[key:<hash>]` (a `facts.StableID` digest of the raw index value, never the
literal key string) for a `for_each` instance, or `[index:<N>]` for a
`count` instance (`identity.go:38-43`). Since the digest is always
hex/alphanumeric, a literal-dot-in-index collision cannot occur through
THIS collector's own data today. `resourceAddressKey` is fixed to be
correct regardless -- the coordinator's directive was to implement the
robust front-stripping parse on its own merits, not merely to patch around
today's one collector's happen-to-be-safe hashing, since a different or
future ingestion path (a fixture built by hand, a different backend
collector) could plausibly carry the literal CLI-display shape.

- No-Regression Evidence (#5572 follow-up, resourceAddressKey fix): `cd go
  && go test ./internal/storage/postgres/... -count=1` is green, including
  the pre-existing `TestPairSpuriousModuleMismatches*` and
  `TestLoadDriftEvidencePairsSpuriousMismatchAcrossModuleResolutionFailure`
  tests (unchanged addresses, no brackets involved, so their expected keys
  are identical under both the old and new `resourceAddressKey`).
  `TestResourceAddressKeyStripsModulePrefixes` was rewritten with the
  reviewer's exact table plus an indexed-module-name case
  (`module.vpc["a.b"].aws_x.y`) and two explicit non-collision assertions;
  it reproduces every reported collision as a genuine RED against the old
  implementation and is GREEN against the new one.
- Observability Evidence (#5572 follow-up, resourceAddressKey fix): no
  metric, span, or read-surface field changed -- `resourceAddressKey` is a
  private pairing-key helper; its output never reaches the API/MCP response
  shape directly, only gates whether `ModuleResolutionReason` mirrors onto a
  state row (already-covered evidence path).

### Golden-corpus snapshot text updated, no assertion or count changed

The `external_registry` golden fixture's `POST
/api/v0/terraform/config-state-drift/findings?variant=derived` entry's
`description` field explicitly (and, before this follow-up, incorrectly)
asserted the state-only half "stays outcome=\"exact\" (state-side rows are
never confidence-flagged)". That sentence was factually wrong after this
fix and was corrected in `testdata/golden/e2e-20repo-snapshot.json` to
describe both halves downgrading to `derived`. No `required_json_values` or
`required_json_object_matches` entry needed to change: both use existential
(`drift_findings[].*`) path semantics (`go/internal/goldengate/query_shape_paths.go`'s
`hasMatchingJSONValue`/`hasMatchingJSONObject` match on ANY array element,
not ALL), so the assertions hold whether the `outcome=derived` filter now
returns one finding (before this fix) or two (after it). `go test
./cmd/golden-corpus-gate/... -count=1` is green against the corrected
snapshot text; `go/cmd/golden-corpus-gate/snapshot_iac_inventory_test.go`
has no drift/outcome-coupled hardcoded counts (checked; this fix changes an
existing finding's `outcome` field value, not the finding count, resource
count, or any IaC inventory aggregate). No live golden-corpus-gate run
accompanies this specific follow-up (the orchestrating PR owner runs it as
part of `make pre-pr` immediately before push, per this repo's
orchestration split between focused-verification authors and the
`make pre-pr` orchestrator).

## Follow-up: two independent review findings the first follow-up missed

A second review pass found TWO further defects, both confirmed against the
committed code before any fix was written, per this repo's
"verify yourself, do not take the reviewer's word for it" convention.

### P1 — the pairing was a silent no-op for every count/for_each resource

Verified directly: `resourceAddressKey("aws_instance.web")` (a config-only
key -- config rows never carry a per-instance index, since
`configRowFromParserEntry` builds addresses from only `resource_type` +
`resource_name`, no instance data) returned `"aws_instance.web"`, while
`resourceAddressKey("module.x.aws_instance.web[index:0]")` (the real state
address for the same resource) returned `"aws_instance.web[index:0]"`
UNCHANGED -- front-stripping (the first follow-up's fix) only ever touched
leading `module.` segments, never the trailing per-instance index. The two
keys never matched, so `pairSpuriousModuleMismatches` never paired ANY
indexed resource, regardless of module resolution confidence.

**Fix:** `resourceAddressKey` now also strips a trailing `[INDEX]` suffix
(new `stripTrailingIndexSuffix`, `tfstate_drift_evidence_pairing.go`), using
the same bracket/quote-depth tracking `skipModuleNameSegment` already uses,
not a naive first-`[` search (a `for_each` key can itself contain a literal
`[`). Two behaviors fall out of this correctly, both proven by test:

- `count = 1` or a single-key `for_each`: exactly one state instance shares
  the stripped key with the one config-only row, so the pairing is
  genuinely unambiguous and fires
  (`TestPairSpuriousModuleMismatchesPairsSingleIndexedStateInstance`).
- `count > 1` or a multi-key `for_each`: every instance strips to the SAME
  key, so the state side has 2+ candidates and the existing unambiguity
  guard correctly REFUSES -- a spurious mismatch genuinely cannot be
  attributed to one of several sibling instances, so this stays "exact"
  rather than "derived" for every instance
  (`TestPairSpuriousModuleMismatchesRefusesWhenMultipleIndexedStateInstancesShareStrippedKey`).
  This is a documented, intentional scope limitation, not a residual bug —
  named explicitly in both `resourceAddressKey`'s and
  `pairSpuriousModuleMismatches`'s doc comments.

- No-Regression Evidence: `cd go && go test ./internal/storage/postgres/...
  -count=1` is green, including
  `TestResourceAddressKeyStripsModulePrefixes`'s extended table (now
  covering `[index:N]`, `[key:<hash>]`, and module-prefixed variants of
  each — RED against the pre-fix implementation, reproducing exactly the
  missing-strip symptom, GREEN after) and the two new
  `TestPairSpuriousModuleMismatches*` cardinality tests above.
- Observability Evidence: none — same private pairing-key helper, no new
  metric, span, or response field.

### P2 — the prior-config confidence's "first-write-wins" guarantee rested on an unguaranteed row order

`loadPriorConfigAddresses` (the second Follow-up section above) relies on
`generationOrder` being most-recent-generation-first so
`collectPriorConfigAddresses`'s first-write-wins map update keeps the
freshest generation's confidence signal. `listPriorConfigAddressesQuery`'s
CTE bounds WHICH generations are included via `ORDER BY ingested_at DESC
LIMIT $3`, but the OUTER SELECT had its own, different `ORDER BY
pg.generation_id ASC, fact.fact_id ASC` — lexicographic on `generation_id
TEXT PRIMARY KEY` (schema/data-plane/postgres/002_scope_generations.sql),
an opaque key with no defined chronological relationship. This codebase's
own `scope_generations_scope_latest_lookup_idx (scope_id, ingested_at DESC,
generation_id DESC)` exists specifically because `generation_id` is NOT
trusted as a recency proxy anywhere else in this codebase — the CTE
comment's "ordered ingested_at DESC — the most recent N" was true of
membership only, never of the returned row order, with or without Postgres
inlining the CTE.

**The committed, re-runnable evidence of record for this fix is
`TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres`**
(`go/internal/storage/postgres/tfstate_drift_evidence_prior_config_ordering_live_test.go`),
gated on `ESHU_POSTGRES_DSN` — see "Committed re-runnable proof" below. The
throwaway `psql`/EXPLAIN session that first found and diagnosed this bug is
preserved below for the diagnostic narrative (how the CTE inlining and the
outer sort key were identified), but it is not itself re-runnable evidence;
do not treat the pasted output below as the proof of record — the committed
test is.

**Investigation, run against a real Postgres 18 instance
(`postgres:18-alpine` in a throwaway Docker container on a private port,
torn down after; schema files `001_ingestion_scopes.sql`,
`002_scope_generations.sql`, `003_fact_records.sql` applied directly via
`psql`, no live-gate script involved), per `eshu-postgres-rigor`'s "prove
the ordering claim rather than asserting it":**

Seeded `repository:repo-a` with three prior generations whose
`generation_id` ASC order differs from their true `ingested_at` order:

```
 generation_id | ingested_at (most recent first)
---------------+---------------------------------
 gen-charlie   | 2026-07-30 08:15:27 (newest)
 gen-alpha     | 2026-07-29 08:15:27
 gen-omega     | 2026-07-28 08:15:27 (oldest)
```

Running the UNMODIFIED, committed `listPriorConfigAddressesQuery` text
returned rows in this order:

```
 generation_id |                              terraform_resources
---------------+--------------------------------------------------------------------------------
 gen-alpha     | [{"path": "main.tf", ...}]
 gen-charlie   | [{"path": "main.tf", ...}]
 gen-omega     | [{"path": "main.tf", ...}]
```

`gen-alpha, gen-charlie, gen-omega` — `generation_id` ASC, NOT the true
recency order `gen-charlie, gen-alpha, gen-omega`. `EXPLAIN (ANALYZE,
VERBOSE, BUFFERS)` on the same query showed:

```
Incremental Sort  (cost=16.39..16.44 rows=2 width=96) (actual time=0.064..0.064 rows=3.00 loops=1)
  Sort Key: scope_generations.generation_id, fact.fact_id
  Presorted Key: scope_generations.generation_id
  ->  Nested Loop  (cost=0.29..16.39 rows=1 width=96) ...
        ->  Index Scan using fact_records_scope_generation_idx on fact_records fact ...
        ->  Limit (cost=0.15..8.17 rows=1 width=40) (actual time=0.003..0.004 rows=3.00 loops=3)
              ->  Index Scan using scope_generations_scope_latest_lookup_idx on scope_generations ...
Planning Time: 6.169 ms
Execution Time: 0.105 ms
```

The `prior_generations` CTE is fully INLINED on Postgres 18 — there is no
separate CTE Scan node; `scope_generations` is scanned directly under the
`Nested Loop`'s inner side, `loops=3` (re-executed once per outer-loop
row), confirming the CTE's own internal ordering is not preserved as a
standalone materialized step. The FINAL `Incremental Sort`'s `Sort Key` is
`generation_id, fact_id` — confirming the OUTER `ORDER BY` clause, not
`ingested_at`, is what actually determines the returned row order,
matching the empirical row order above exactly.

**Fix:** exposed `ingested_at` from the CTE and changed the outer `ORDER
BY` to `pg.ingested_at DESC, pg.generation_id ASC, fact.fact_id ASC`.
Re-run against the same seeded data:

```
 generation_id |                              terraform_resources
---------------+--------------------------------------------------------------------------------
 gen-charlie   | [{"path": "main.tf", ...}]
 gen-alpha     | [{"path": "main.tf", ...}]
 gen-omega     | [{"path": "main.tf", ...}]
```

Correct: `gen-charlie, gen-alpha, gen-omega`, matching true `ingested_at`
recency. `EXPLAIN (ANALYZE, VERBOSE, BUFFERS)` on the fixed query:

```
Incremental Sort  (cost=16.38..16.43 rows=2 width=104) (actual time=0.085..0.086 rows=3.00 loops=1)
  Sort Key: scope_generations.ingested_at DESC, scope_generations.generation_id, fact.fact_id
  Presorted Key: scope_generations.ingested_at
  ->  Nested Loop  (cost=0.29..16.38 rows=1 width=104) ...
        ->  Limit (cost=0.15..8.17 rows=1 width=40) (actual time=0.009..0.010 rows=3.00 loops=1)
              ->  Index Scan using scope_generations_scope_latest_lookup_idx on scope_generations ...
        ->  Index Scan using fact_records_scope_generation_idx on fact_records fact ...
Planning Time: 6.225 ms
Execution Time: 0.139 ms
```

Same cost (16.38 vs 16.39 — statistically identical), same
`scope_generations_scope_latest_lookup_idx` index, no new index needed —
this index's own column order `(scope_id, ingested_at DESC, generation_id
DESC)` already matches the new requirement exactly, so the inner scan's
`Presorted Key: scope_generations.ingested_at` now feeds the outer sort
almost for free. No query-plan regression; this is a pure correctness fix.

### Committed re-runnable proof (review follow-up: close the P2 evidence gap)

The first version of this fix had no committed evidence that could
re-detect a regression at the Postgres planner level -- only the throwaway
session above and a substring assertion on the SQL constant. Review
correctly flagged that gap: a substring test cannot catch a syntactically
different `ORDER BY` that still contains the matched text but produces the
wrong order, and it cannot catch a planner-level regression (e.g. the
query stops using `scope_generations_scope_latest_lookup_idx`, or Postgres
starts materializing the CTE) at all.

`TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres`
(new, `tfstate_drift_evidence_prior_config_ordering_live_test.go`) converts
the throwaway proof into a permanent, DSN-gated integration test, following
this package's established live-Postgres pattern (`ESHU_POSTGRES_DSN`,
isolated `CREATE SCHEMA`/`SET search_path`, `MigrationSQL` for the three
migrations the query depends on -- mirrors
`openStaticGrantPolicyHashLiveSchema` and `openProviderConfigLiveSchema`):

- Seeds THREE prior generations (the same shape the throwaway proof used,
  since that is what actually distinguishes the two orderings) whose
  `generation_id` lexical order (`gen-alpha, gen-charlie, gen-omega`) is
  scrambled relative to their true `ingested_at` order (`gen-charlie`
  newest, `gen-alpha` middle, `gen-omega` oldest). `gen-charlie` (the most
  recent) carries a registry-shorthand module-resolution failure
  (`ModuleResolutionReason = "external_registry"`); the other two resolve
  cleanly.
- Calls `loadPriorConfigAddresses` directly against the real Postgres
  connection and asserts `out["aws_instance.web"] == "external_registry"`
  -- only correct if the query's row order genuinely lets the most recent
  generation win first-write-wins.
- Caps the handle at one connection
  (`db.SetMaxOpenConns(1)`/`db.SetMaxIdleConns(1)`), mirroring
  `TestLatestGenerationCTETruthEquivalenceAndPlan`'s identical guard:
  `SET search_path` is connection-local, and a `*sql.DB` is a pool that can
  silently hand a later query a different, unconfigured connection if left
  uncapped -- the exact failure mode issue #4451 hit.
- Additionally asserts (mirroring `TestLatestGenerationCTETruthEquivalenceAndPlan`'s
  `SubPlan`-node assertion, the only existing fixture-plan pattern in this
  package) that the `EXPLAIN` output contains no `CTE Scan` node -- a cheap
  plan-shape corroboration, not an ordering proof by itself, since both the
  broken and fixed query text inline identically on Postgres 18+; it guards
  against a future edit that accidentally forces materialization.

**RED, reproduced by temporarily restoring the pre-fix outer `ORDER BY
pg.generation_id ASC, fact.fact_id ASC` and running the new test against a
real Postgres 18 instance:**

```
=== RUN   TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres
    tfstate_drift_evidence_prior_config_ordering_live_test.go:71: out["aws_instance.web"] = "", want "external_registry" — the most recently ingested prior generation (gen-charlie) carries the flagged confidence; getting anything else (typically "" from an older, clean generation winning first) means listPriorConfigAddressesQuery's row order is not genuinely most-recent-first
--- FAIL: TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres (0.28s)
FAIL
```

**GREEN after restoring the fixed `ORDER BY pg.ingested_at DESC,
pg.generation_id ASC, fact.fact_id ASC`, same seeded data, same Postgres
instance:**

```
=== RUN   TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres
--- PASS: TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres (0.37s)
PASS
```

`git diff go/internal/storage/postgres/tfstate_drift_evidence_sql.go`
confirmed clean (no diff) after the temporary revert-and-restore cycle --
the committed query text was never actually changed.

Skip behavior confirmed with `ESHU_POSTGRES_DSN` unset (the credential-free
CI / `make pre-pr` path):

```
=== RUN   TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres
    tfstate_drift_evidence_prior_config_ordering_live_test.go:53: set ESHU_POSTGRES_DSN to run the real-Postgres prior-config ordering proof
--- SKIP: TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres (0.00s)
PASS
```

- No-Regression Evidence:
  `TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres`
  (above) is the committed, re-runnable real-Postgres proof; it is not run
  by default `go test ./...` (skips cleanly without `ESHU_POSTGRES_DSN`),
  so it does not affect credential-free CI or `make pre-pr`, but it is the
  evidence of record for this specific ordering claim, re-provable any time
  a reviewer sets the DSN.
  `TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending` is kept
  as a cheap, credential-free complement -- it asserts the SQL constant
  text contains the correct outer `ORDER BY` — RED against the pre-fix text
  (which lacked `ORDER BY pg.ingested_at DESC` on the outer SELECT
  entirely), GREEN after. This is a text assertion, not a fakeExecQueryer
  behavioral test, because fakeExecQueryer bypasses real SQL execution and
  returns whatever row order a test hands it regardless of the query text
  -- only a real Postgres planner run can prove or disprove a row-ordering
  claim, which is exactly the gap the new live test closes.
  `TestCollectPriorConfigAddressesFirstWriteWinsDependsOnCallOrder` proves,
  directly and without any DB fixture, that reversing the call order on the
  SAME two conflicting entries flips the winning reason — the mechanism the
  SQL fix protects.
  `TestPostgresDriftEvidenceLoaderPrefersMostRecentPriorGenerationConfidenceOnConflict`
  is the first fakeExecQueryer-based regression in this package to exercise
  TWO prior generations declaring the same address with conflicting
  confidence (none existed before, which is why this went unnoticed); it
  feeds the fixture rows in the fixed query's guaranteed order and proves
  the full loader wiring (per-generation `buildModulePrefixMap`,
  `mergeDriftRows`'s promotion) carries the correct, most-recent
  generation's reason through to the durable `AddressedRow`. `cd go && go
  test ./internal/storage/postgres/... -count=1` is green (DSN unset, so
  the new live test skips in this run; separately re-run with
  `ESHU_POSTGRES_DSN` set against a scratch Postgres 18 instance and
  confirmed green above).
- Observability Evidence: none — pure query-text and Go-side ordering fix,
  no new field, metric, span, or log line; the existing
  `logPriorConfigWalk` INFO log is unaffected.

### Doc comments corrected (review finding #4)

`resourceAddressKey`'s and `pairSpuriousModuleMismatches`'s doc comments
previously claimed the unambiguity guard "bounds false pairings to zero"
unconditionally, and that the key "recovers... the last two dot-separated
segments... regardless of index suffix" — both false, as the two defects
above prove. Both doc comments now state the real, narrower guarantee
(same key implies same logical resource and vice versa, GIVEN a correct
resourceAddressKey; the cardinality check itself never manufactures a false
pairing on top of that) and name the known gaps explicitly: the
count/for_each multi-instance miss (intentional, documented above), and the
unescaped-double-quote-inside-a-quoted-index edge case (vanishingly rare,
fails closed rather than silently misparsing).

## Schema-versioning decision (owner call, review round 4)

Codex raised a P1 on `terraform_config_state_drift_writer.go`: emitting the new
closed-label value `derived` while the fact envelope stays schema version
`1.0.0`, and simultaneously narrowing what `exact` asserts, is classified as a
major change by `docs/internal/design/contract-system-v1.md` ("Major = remove/
rename key, narrow a type, change stable-key derivation, change meaning").

The citation is accurate, and the alternatives were real: a schema major
`2.0.0` plus a decoder compatibility shim, or keeping v1 semantics and carrying
the signal only in an additive optional field. The repo owner decided: **no
major bump; treat it as a correction.**

Recorded here and in `sdk/go/factschema/reducerderived/v1/findings.go` so the
reasoning survives, rather than living only in a resolved review thread:

- #5572 exists *because* `exact` was already wrong on heuristically-addressed
  findings. Those rows were never entitled to the label, so aligning the
  implementation with what `exact` was always meant to assert is a correction,
  not a redefinition of a previously-honest value.
- The policy's payload-schema major rule governs the external
  collector-to-core boundary. `reducer_terraform_config_state_drift_finding`
  sits in `reducer_domain: reducer_derived_findings` -- produced by the reducer,
  consumed by Eshu's own query/MCP layers -- and does not cross it.
- `sdk/go/factschema/schema/reducer_terraform_config_state_drift_finding.v1.schema.json`
  types `outcome` as an unconstrained string with no enum, so no schema-derived
  validator rejects `derived`, and the `verify-contracts` and payload-usage
  manifest gates see no field change.
- The read surface is `/api/v0`, explicitly pre-stable, and its OpenAPI, the MCP
  tool contract, and the capability matrix were all updated in this PR.

Honest limit of that last point: neither the schema-diff gate nor the
payload-usage manifest can detect a *semantic* narrowing of an existing field's
value domain. Their passing is not evidence that no break occurred -- it only
confirms the break, if one exists, is of the kind those gates were never able to
catch. The decision rests on the reasoning above, not on the green gates.

A consumer that read `outcome == "exact"` as "every classified per-address
finding" should now read `outcome IN ("exact","derived")` for that meaning.
