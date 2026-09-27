# Tier and OCI Batch-Read Cardinality Evidence (#6590)

Two `keyed_support bounded_key_batch` query-source-coverage rows recorded
`max_results` numbers that rested on unenforced per-key cardinality
assumptions: the blast-radius Tier lookup (`enrichBlastRadiusTiers`) and the
OCI trio (`fetchOCIImageTagRows`, `fetchOCIImagesByDigest`,
`fetchOCIRepositoriesByUID`, `go/internal/query/impact/trace_deployment_oci.go`).
This closes the issue's tracked decision: enforce what code can enforce
(`fetchOCIRepositoriesByUID`'s uniqueness constraint, and a fail-closed
writer guard for Tier), raise the OCI trio's read/digest rows to honest,
disclosed-unenforced numbers with no code change, and fix
`enrichBlastRadiusTiers`'s silent last-write-wins bug on the genuinely
unenforced case.

## Decision history (why no cap-and-truncate)

The issue owner's 2026-09-07 correction (quoted verbatim) rules out a silent
result cap for the OCI trio:

> Correction to the decision above after tracing the call chain: silent
> truncation is NOT safe here. Two mechanisms: (1) buildOCITagTruthRows emits
> no missing-key marker, so a cap-dropped ref vanishes exactly like a
> never-observed ref; (2) worse, it groups rows per ref and marks multi-row
> groups ambiguous — dropping one row of a pair flips a ref from ambiguous to
> seemingly-unambiguous, silently destroying the weak_tag signal. [...]
> Revised recommendation: (b) honest raised numbers now (no code change),
> (a-with-disclosure) as designed follow-up work.

This change implements (b) for the two OCI rows that stay genuinely
unenforced (`fetchOCIImageTagRows`, `fetchOCIImagesByDigest`), tracked as a
follow-up under #6590 itself in their YAML comments. It implements the
enforceable half of (a) --without-disclosure, since no result truncation is
introduced-- for `fetchOCIRepositoriesByUID` (a real DB uniqueness
constraint) and for the Tier lookup (a fail-closed Go-side write guard, since
no Tier writer exists yet to enforce anything in the schema).

## OCI per-key cardinality (verified facts)

- `ContainerImageTagObservation` uid is `(repository_id, tag,
  resolved_digest)` (`canonicalOCIImageTagObservationUpsertCypher` in `go/internal/storage/cypher/oci_registry_canonical_writer.go`
  builds `canonicalOCIImageTagObservationUpsertCypher`, which `MERGE`s on that
  uid), so one `image_ref` can legitimately have one row per digest the tag
  ever resolved to. `buildOCITagTruthRows`
  (`buildOCITagTruthRows` in `go/internal/query/impact/trace_deployment_oci.go`) depends on that
  multiplicity: it groups tag-lookup rows by `image_ref` and marks a group
  `ambiguous` the moment it sees more than one distinct digest.
- `ContainerImageDescriptor`'s uid embeds the repository
  (`NormalizeDescriptorIdentity` in `go/internal/collector/ociregistry/identity.go`,
  `fmt.Sprintf("oci-descriptor://%s/%s@%s", repository.Registry,
  repository.Repository, digest)`), so a single digest can resolve to more
  than one descriptor across repositories.
- Only `OciRegistryRepository.uid` carries a uniqueness constraint
  (the `oci_registry_repository_uid_unique` constraint, asserted by `TestSchemaStatementsContainsUIDConstraints` in `go/internal/graph/schema_test.go`,
  `oci_registry_repository_uid_unique`); `digest`
  (`container_image_digest`/`container_image_descriptor_digest`) and
  `image_ref` (`container_image_tag_observation_ref`) are plain, non-unique
  indexes (`container_image_digest` and `container_image_tag_observation_ref` in `schemaPerformanceIndexes`, `go/internal/graph/schema_tables_indexes.go`).
- `fetchOCIRepositoriesByUID` batches on `OciRegistryRepository.uid`
  (`fetchOCIRepositoriesByUID` in `go/internal/query/impact/trace_deployment_oci.go`), so that
  constraint is a genuine per-key fan-out-1 enforcer.
  `fetchOCIImageTagRows`/`fetchOCIImagesByDigest` batch on `image_ref` and
  `digest`, neither of which is unique, so their existing 250/750 numbers
  describe an observed worst case, not an enforced one.
- A fail-closed error path is not viable for `FetchOCIImageRegistryTruth`
  today: an error there 500s the whole deployment-chain trace
  (the `FetchOCIImageRegistryTruth` error path in `(*Handler).TraceDeploymentChain`, `go/internal/query/impact/trace_deployment.go`), and there is no
  corpus measurement to size a safe cap.

## Tier per-key cardinality (verified facts)

- `blastRadiusTierLookupCypher`
  (`blastRadiusTierLookupCypher` in `go/internal/query/impact/blast_radius.go`) is a single
  `MATCH (a:Repository)<-[:CONTAINS]-(tier:Tier) WHERE a.id IN $repo_ids
  RETURN ...` with no `LIMIT` and no `DISTINCT`.
- No `:Tier` writer exists anywhere in non-test Go under `go/cmd` or
  `go/internal`; the only two `:Tier` references outside this read are the
  `tier_name` uniqueness constraint on `Tier.name`
  (the `tier_name` constraint in `schemaConstraints`, `go/internal/graph/schema_tables.go`) and this read itself. A
  constraint on `Tier.name` says nothing about how many `Tier` nodes a given
  `Repository` can be `CONTAINS`-linked from.
- ops-qa read-only status (owner's issue comment, 2026-09-26/27): **0 `:Tier`
  nodes and 0 `(:Tier)-[:CONTAINS]->(:Repository)` edges** in the reference
  corpus. The assumption of at most one tier per repo holds only vacuously
  today.
- `enrichBlastRadiusTiers` (pre-fix) built `tiers := map[string]map[string]string`
  keyed by `repo_id` from the lookup rows with no dedup and no conflict
  detection: a second row for the same `repo_id` silently overwrote the
  first. A repo with two `(tier, risk)` candidates got whichever one the
  driver returned last, not a defined choice.

## Fix 1 -- validator: `fan_out_multiplier` on `keyed_support bounded_key_batch` rows

`NonHotDisposition` gains an optional `fan_out_multiplier` field
(`go/internal/queryplan/source_coverage.go`). When set on a
`keyed_support`/`bounded_key_batch` row, `max_results` must equal
`max_keys * fan_out_multiplier` exactly; the field is rejected on any other
class or key-bound (`go/internal/queryplan/source_coverage_non_hot.go`,
`validateFanOutMultiplier`). `validateNonHotDisposition` and
`validateNonHotMaxDegree` moved verbatim from `source_coverage.go` into the
new `source_coverage_non_hot.go` (a pure move -- proof: `go test
./internal/queryplan -count=1` passes identically before and after the move,
before the fan-out field or checks existed).

Two rows were migrated to declare `fan_out_multiplier: 1`:
`(*Handler).enrichBlastRadiusTiers` (`impact/blast_radius.go`, enforced by
the new Tier writer guard below) and `fetchOCIRepositoriesByUID`
(`impact/trace_deployment_oci.go`, enforced by
`oci_registry_repository_uid_unique`). The other 17 pre-existing
`bounded_key_batch` rows are untouched.

RED (field does not exist yet):

```
$ go test ./internal/queryplan -run 'FanOut' -count=1 -v
internal/queryplan/source_coverage_fanout_test.go:29:5: unknown field FanOutMultiplier in struct literal of type NonHotDisposition
[...5 more occurrences...]
FAIL	github.com/eshu-hq/eshu/go/internal/queryplan [build failed]
FAIL
$ echo RC=$?
RC=1
```

GREEN (after adding the field, the pure move, and the fan-out checks):

```
$ go test ./internal/queryplan -run 'FanOut' -count=1 -v
--- PASS: TestValidateSourceCoverageRejectsFanOutMultiplierOutsideBoundedKeyBatch (0.00s)
    --- PASS: .../degree_bounded_class (0.00s)
    --- PASS: .../keyed_support_single_key (0.00s)
    --- PASS: .../label_inventory_class (0.00s)
--- PASS: TestValidateSourceCoverageRejectsFanOutMultiplierBelowOne (0.00s)
--- PASS: TestValidateSourceCoverageRejectsFanOutMultiplierMismatch (0.00s)
--- PASS: TestValidateSourceCoverageAcceptsFanOutMultiplier (0.00s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/queryplan	0.080s
$ echo RC=$?
RC=0
```

Seeded-violation mutation proof against the real manifest (planted
`max_results: 201` on the `enrichBlastRadiusTiers` row, `max_keys: 200`,
`fan_out_multiplier: 1`):

```
$ go test ./internal/queryplan -run 'TestHotCypherManifestCoversEveryProductionQueryCall' -count=1 -v
source_coverage_test.go:450: production query source coverage:
impact/blast_radius.go:(*Handler).enrichBlastRadiusTiers: bounded_key_batch
requires max_results == max_keys x fan_out_multiplier (200 x 1 = 200, got
201); max_results is derived, not picked
--- FAIL: TestHotCypherManifestCoversEveryProductionQueryCall (0.15s)
FAIL
$ echo RC=$?
RC=1
```

Reverted to `max_results: 200`; `go test ./internal/queryplan -count=1` is
green again (RC=0) and `git diff --stat` on the YAML shows no residual
change.

## Fix 2 -- Tier single-membership writer guard

`go/internal/storage/cypher/tier_writer_scan_test.go` adds
`TestNoTierWriterWithoutSingleMembershipContract`: a repo-wide static scan
(the same `filepath.Walk` shape as
`merge_then_create_repo_scan_test.go`'s `scanForNodeMergeThenCreate`,
reusing its `buildFileConstIdentMap`/`foldStringExpr`/`mergeOpenPattern`/
`createClausePattern` helpers) that fails the build the moment any non-test
`.go` file under `go/cmd` or `go/internal` writes the `:Tier` label (node
`MERGE`/`CREATE`, a matched-then-membership-`MERGE`, or a dynamic `SET
n:Tier`) without a matching `tierSingleMembershipWriters["path:line"]` entry,
or contains a bare Go string literal `"Tier"` (a possible dynamic-label
write) outside `tierDynamicLabelAllowlist`. Both maps are empty today except
the one pre-existing allowlisted hit,
`internal/replay/recordpseudo/walker.go:17` (an AWS tag-key vocabulary
list, not Cypher).

`TestWritesTierLabel` unit-tests the write-shape predicate on the exact
RED/GREEN table from the design:

```
$ go test ./internal/storage/cypher -run 'Tier' -count=1 -v
--- PASS: TestWritesTierLabel (0.00s)
    --- PASS: .../node_MERGE_with_Tier_label (0.00s)
    --- PASS: .../node_CREATE_with_Tier_label (0.00s)
    --- PASS: .../match_existing_Tier_then_merge_membership_edge (0.00s)
    --- PASS: .../dynamic_SET_label (0.00s)
    --- PASS: .../lowercase_merge_keyword_across_a_newline (0.00s)
    --- PASS: .../constraint_DDL (0.00s)
    --- PASS: .../differently_labeled_merge (0.00s)
    --- PASS: .../tier_as_a_property,_not_a_label (0.00s)
    --- PASS: .../pure_read_of_Tier (0.00s)
--- PASS: TestNoTierWriterWithoutSingleMembershipContract (0.77s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/storage/cypher	0.979s
$ echo RC=$?
RC=0
```

Seeded-violation mutation proof #1 (planted `const tierProbe = "MERGE
(t:Tier {name: $n})"` in `oci_registry_canonical_writer.go`, right after the
`canonicalPhaseOCIRegistry` const):

```
$ go test ./internal/storage/cypher -run 'TestNoTierWriterWithoutSingleMembershipContract' -count=1 -v
tier_writer_scan_test.go:198: found 1 :Tier write(s) with no registered
single-membership contract in tierSingleMembershipWriters (#6590): [...]
Violations:
  internal/storage/cypher/oci_registry_canonical_writer.go:15
--- FAIL: TestNoTierWriterWithoutSingleMembershipContract (1.42s)
FAIL
$ echo RC=$?
RC=1
```

Seeded-violation mutation proof #2 (planted `var tierLabel = "Tier"` at the
same location, after reverting proof #1):

```
$ go test ./internal/storage/cypher -run 'TestNoTierWriterWithoutSingleMembershipContract' -count=1 -v
tier_writer_scan_test.go:208: found 1 Go string literal(s) with value
exactly "Tier" outside tierDynamicLabelAllowlist: [...]
Violations:
  internal/storage/cypher/oci_registry_canonical_writer.go:15
--- FAIL: TestNoTierWriterWithoutSingleMembershipContract (1.34s)
FAIL
$ echo RC=$?
RC=1
```

Both probes reverted; `git diff --stat
go/internal/storage/cypher/oci_registry_canonical_writer.go` is empty
(RC=0) and `go test ./internal/storage/cypher -run 'Tier' -count=1` is green
again.

## Fix 3 -- `enrichBlastRadiusTiers` withholds ambiguous tier/risk

`go/internal/query/impact/blast_radius_tier_cardinality_test.go` adds
`TestEnrichBlastRadiusTiersWithholdsAmbiguousTier`, driving
`(*Handler).enrichBlastRadiusTiers` directly against a fake `Neo4j.Run` that
returns `repo-web -> {tier-1, critical}` and `{tier-2, low}` (two distinct
pairs -- ambiguous) and `repo-api -> {tier-1, critical}` twice (one distinct
pair, duplicated -- not ambiguous).

RED (pre-fix `enrichBlastRadiusTiers`, last-write-wins map):

```
$ go test ./internal/query/impact -run 'TestEnrichBlastRadiusTiersWithholdsAmbiguousTier' -count=1 -v
blast_radius_tier_cardinality_test.go:51: ambiguous repo-web row must
withhold tier, got map[string]interface {}{"repo_id":"repo-web",
"risk":"low", "tier":"tier-2"}
--- FAIL: TestEnrichBlastRadiusTiersWithholdsAmbiguousTier (0.00s)
FAIL
$ echo RC=$?
RC=1
```

GREEN (after adding `blastRadiusRepoTiers`, which groups rows per `repo_id`,
collapses duplicate identical pairs, and withholds + logs any repo with more
than one distinct pair):

```
$ go test ./internal/query/impact -run 'BlastRadius' -count=1 -v
[... all TestFindBlastRadius*, TestBlastRadiusQueriesAreNornicDBSafe,
TestEnrichBlastRadiusTiersWithholdsAmbiguousTier, and 5 more --- PASS ...]
PASS
ok  	github.com/eshu-hq/eshu/go/internal/query/impact	0.467s
$ echo RC=$?
RC=0
```

`go test ./internal/query/impact -count=1` (whole package): RC=0.

## OCI trio YAML comments (no code change)

`fetchOCIImageTagRows` and `fetchOCIImagesByDigest` keep their existing
250/750 numbers and gain a comment disclosing they are unenforced and why,
citing #6590 as the tracker for the OCI result-bound follow-up the
2026-09-07 decision records. `fetchOCIRepositoriesByUID` gains
`fan_out_multiplier: 1` with a comment naming
`oci_registry_repository_uid_unique` as the enforcer; its `max_results: 250`
is unchanged (`250 * 1 == 250`) and its `source_sha256` is unchanged (no
code touched).

No-Regression Evidence: `go test ./internal/queryplan -count=1`,
`go test ./internal/query/impact -count=1`, and
`go test ./internal/storage/cypher -run 'Tier' -count=1` are all green after
every fix above, with the RED captures and reverted mutation proofs shown
per fix. `enrichBlastRadiusTiers`'s statement text is byte-identical
(`blastRadiusTierLookupCypher` unchanged); the fix is Go-side grouping only,
still O(rows) over the same lookup result, so no Cypher shape or plan
changed and no scaled benchmark is claimed.

Observability Evidence: `enrichBlastRadiusTiers` now logs
`h.Logger.Warn("blast-radius tier enrichment ambiguous; withholding tier",
"repo_id", id, "tier_candidates", n)` for every repo whose tier resolves
ambiguously, in addition to the pre-existing lookup-error warning. This is
the only new operator signal in this change; everything else is a static
CI-time guard (`TestNoTierWriterWithoutSingleMembershipContract`, the
`fan_out_multiplier` validator check) with no runtime footprint.
