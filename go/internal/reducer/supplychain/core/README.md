# core

The supplychain family: supply-chain impact correlation plus vulnerability
suppression (issue #6061, step 2 of
`docs/internal/design/reducer-target-tree.md`).

## Ownership

This package owns the join from vulnerability-intelligence facts to impacted
repositories and workloads: it reads CVE, affected package/product, OS
package, EPSS, KEV, and suppression facts, package-registry and
manifest/lockfile dependency evidence, SBOM and container-image identity
evidence, repository facts, CI run correlation output, and provider security
alerts, and writes reducer-derived supply-chain impact findings plus
suppression decisions. The reducer runtime drives `SupplyChainImpactHandler`
through the default domain catalog; `go/cmd/reducer` wires the Postgres
writer. Query surfaces read the durable facts; they never import this
package.

For package advisories, an attached SBOM image path preserves package and
image evidence while the ecosystem matcher decides version truth. In-range
versions are `affected_exact`; known fixed versions remain
`not_affected_known_fixed`; missing, malformed, conflicting, or outside-range
versions remain `possibly_affected`. The product/CPE-only image path keeps its
separate `affected_derived` rule. The existing finding payload and counters
expose these decisions without a new queue stage or metric label. One package
advisory can now produce separate findings for every verified SBOM image
subject and installed version. An unassociated package manifest or OS scan
remains its own finding; a scanner result joins an SBOM image only when the
scan scope and generation resolve that image's digest. Conflicting
scanner digests for one scope and generation resolve no subject. Conflicting
vendor OS versions for the same resolved image stay partial with both scan
facts in provenance. Equivalent image rows coalesce after version conflict
checks. One complete writer pass publishes the resulting set so prior subjects
can retract when evidence changes.

Performance Evidence (#9232): `BenchmarkSBOMVersionGuard` uses the same
five-fact npm advisory and attached image on Apple M5 Max with Go 1.26.10.
The single-subject baseline was commit `2446f21874245d2d63a46ecf28136dfed44dfa64`.
The previous single-subject classifier measured 9592/9666/9715 ns/op (165
allocations/op). The final multi-subject classifier measured
11155/12777/11430 ns/op (167 allocations/op); its additional subject
indexing has a small measurable cost on this input. The new fan-out benchmark
measured 11244/12797 ns/op for one subject, 33096/37685 ns/op for two, and
309411/331932 ns/op for sixteen (two one-second samples each). These runs
bound the local cost shape; they do not claim a throughput target. The writer
still publishes the full finding set in one transaction, with no new worker,
lease, queue, or graph write.

Performance Evidence (#9232 OS lookup): `BenchmarkSBOMSubjectOSLookup`
prebuilds the index with one matching SBOM and vendor OS row, then times only
package classification with 0, 100, or 10,000 unrelated OS keys. On Apple M5
Max with Go 1.26.10, three 500 ms samples at baseline `a6933d2878` measured
6,793/6,805/6,836 ns/op with no unrelated keys and
363,215/361,581/363,002 ns/op with 10,000. The keyed lookup measured
6,948/6,863/7,064 and 6,069/6,035/6,001 ns/op on those same inputs.
The unrelated corpus no longer determines classification cost; this local
microbenchmark excludes index construction, database work, and queue time.

Observability Evidence (#9232): the existing persisted impact finding carries
`impact_status`, `match_reason`, `observed_version`, `subject_digest`, and
`evidence_fact_ids`; the existing query span and reducer finding counters
expose the read and classification. No new metric or span is added.

Alert-derived findings carry the provider's 0–1 EPSS percentage and percentile
when each value is a valid decimal. The collector converts GitHub's numeric
percentage and string percentile into fact strings before this package decodes
them.

No-Regression Evidence (#9231): `BenchmarkBuildSupplyChainImpactFindingsAlertEPSS`
on Apple M5 Max, Go 1.26.10, same two-envelope input, three one-second samples:
baseline 7973/7859/7884 ns/op and 118 allocs/op; changed 7960/8066/7988
ns/op and 117 allocs/op. The ranges overlap and the changed samples are within
2.6% of baseline. This is no measured material regression on the touched path;
it does not prove an end-to-end throughput improvement. The benchmark has no
backend or queue; it returns one finding per call.

Observability Evidence (#9231): alert-derived findings now carry validated EPSS
fields in the existing persisted finding and HTTP read. The RED/GREEN fixture
checks that output. No metric, span, log key, or new queue stage was added.

Performance Evidence (#9229): `BenchmarkBuildGoReachabilityAliasIndex` on
Apple M5 Max with Go 1.26.10 and the same 200 GO advisories, each with a
distinct CVE alias, measured 354876/360197/356569 ns/op (789913 B/op, 6303
allocs/op) at `origin/main` and 389028/383986/393318 ns/op (874137 B/op,
6894 allocs/op) after the alias join. The added 24–38 microseconds per 200
advisories is the cost of indexing the extra identity; this index builds once
per reducer intent, with no new backend calls or nested advisory scan. The
benchmark is in memory and does not establish end-to-end throughput.

No-Observability-Change (#9229): the finding's existing runtime reachability
and evidence fields now carry matched GO call proof through a CVE alias. The
reducer's existing execution metrics and input-invalid quarantine remain the
operator signals; no new worker, queue stage, metric label, span, or runtime
setting was added. QA behavior is not observable until Go target derivation
exists.

The directory stays whole. The `Suppression` field on
`SupplyChainImpactFinding` makes the finding, core, and suppression code one
unit: the suppression functions take the finding type (#6636). A later move,
such as the `supply/chain/impact` leaf in the reducer target tree, must move
all three together.

## EPSS selection

For each CVE, the reducer chooses the latest valid EPSS score date. Equal dates
use observation time, stable fact identity, then payload values for duplicate
identities; undated legacy observations
rank below dated ones. Probability, percentile, score date, optional model
version, and evidence fact ID remain one observation. Invalid dates are
quarantined with the input-invalid signal. KEV remains independent.

No-Regression Evidence: The two-day fixture is evaluated in both input orders,
with same-day ties, undated legacy data, invalid dates, and independent KEV.
The same-input benchmark covers the EPSS indexing path before and after the
change.

Observability Evidence: The existing per-fact input-invalid dead-letter metric
and structured log identify malformed date facts; selected date and optional
model version are available in finding provenance.

## Files

Non-test files fall into these groups: the `impact*` and other short-named
files (impact builders, ecosystem matchers, reachability, anchoring, writers),
the suppression-story files (`evaluation.go`, `decode.go`, `reasons.go`,
`scope.go`), and the `go_reachability*` files (Go module/call reachability
classifier). Test files sit beside the code they cover. No file repeats
the directory name.

| File | Covers |
| --- | --- |
| `impact.go` | `SupplyChainImpactHandler`, finding pipeline |
| `finding.go` | `SupplyChainImpactFinding` (carries the `Suppression` field: impact + suppression are one unit) |
| `writer.go` | `PostgresSupplyChainImpactWriter`, batched versioned inserts |
| `writer_retract.go` | Replace-set transaction (#6831): per-(scope, generation) advisory lock, upsert, tombstone of superseded findings |
| `winners_maintainer.go` | `SupplyChainImpactWinnersMaintainer` read-model resweep |
| `evaluation.go`, `decode.go`, `reasons.go`, `scope.go` | Suppression story: Decision + Evaluate, decode, reasons, scope match |
| `go_reachability*.go` | Go module/call reachability classification |
| `cross_scope_test_doubles_test.go` | Family-local test doubles (each side keeps its own copy) |

## Seams other families use

- Builders: `BuildSupplyChainImpactFindings`,
  `BuildSupplyChainImpactRemediation`, `BuildVulnerabilitySuppressions`,
  `EvaluateSupplyChainSuppression`, `ClassifyGoVulnerabilityReachability`.
- Handler/writers: `SupplyChainImpactHandler`, `SupplyChainImpactWriter`,
  `PostgresSupplyChainImpactWriter`, `SupplyChainImpactWinnersMaintainer`,
  `SupplyChainImpactBeginner`/`SupplyChainImpactTx` (the writer's
  transaction seam).

## Write contract

Each pass's finding set is the complete truth for its `(scope, generation)`.
`PostgresSupplyChainImpactWriter` upserts it and tombstones (never deletes)
every other active finding row of that `(scope, generation)` in one
transaction, behind a `pg_advisory_xact_lock` on that pair, so a superseded
finding -- for example a repo-less row replaced once repository anchoring
arrived -- stops being served (#6831). A pass whose evidence load stopped
short for a cause that can change a finding's identity -- the 8-round
expansion cap or the per-intent evidence budget --
(`SupplyChainImpactWrite.PartialEvidence`) upserts only. The per-key caps the
OS-package, scanner-analysis-scope, resolved-digest and peer-identity stages
used to carry are paged to completion, and a bounded suppression tail does not
block retraction, so a scope larger than one page converges (#7154). Evidence
and measurements:
`docs/internal/evidence/6831-supply-chain-impact-replace-set.md`,
`docs/internal/evidence/7154-capped-scope-convergence.md`.

Truncation is recorded by cause (`evidence_truncation.go`): `active_expansion_rounds`
(the 8-round expansion cap) and `evidence_budget` (the per-intent expansion
envelope budget, default 100,000) set `PartialEvidence`; `suppression_tail`
(only the `vulnerability.suppression` tail of a read was bounded) does not,
because a suppression is not part of a finding's identity and core evidence is
always loaded in full before the tail is counted, so that pass still retracts
and only discards its suppression candidates. The OS-package read is narrowed to
the intent's affected-package lookup keys and drained in one snapshot
(`ListOSPackageAdvisoryFactEnvelopes(ecosystems, packageIDs, limit)`); the
digest and repository-id filters read in chunks of 256. Operators see a stuck scope through
`eshu_dp_supply_chain_impact_evidence_truncated_total{domain,reason}` and one
WARN log line naming the scope and generation.

Each active-evidence call resolves matching fact IDs once from the
index-anchored statements in `storage/postgres/facts/impact`, orders core
then suppression IDs by byte order, keeps the first 2,000 suppressions (more
sets the suppression-tail bool), and fetches payloads in 500-ID primary-key
pages in one snapshot (#8880). The handler prefers the loader's
`ListActiveSupplyChainImpactFactsWithStats` surface and sums its per-statement
rows and seconds into the pass's success-line sub-signals
(`active_evidence_branch_<name>_rows`, `active_evidence_resolved_fact_ids`,
`active_evidence_payload_pages`, `active_evidence_guard_rows`). Evidence:
`docs/internal/evidence/8880-supply-chain-impact-id-resolution.md`.

Each pass is also fenced (#7142). `Handle` issues a database-issued token from a
Postgres sequence before the evidence load (`SupplyChainImpactFencingTokenIssuer`);
the writer takes the advisory lock, then runs a compare-and-set on
`supply_chain_impact_write_admission` (a pass whose token is older than one
already admitted is rejected whole with the retryable, non-counting
`supply_chain_impact_write_superseded` error), then upserts rows stamped with the
token, then retracts with `fencing_token <= token` and stamps the tombstones with
it. A missing issuer or a zero token fails closed. A scope that stays superseded
means two workers keep overtaking each other or the sequence lags the admitted
watermark after a restore or manual reset; the repair is to stop every reducer replica, run the migration 154 seed by hand and restart them (the seed is not live-safe), as the evidence note describes. Rolling back to a reducer that predates the token freezes the
rows the new reducer stamped; the evidence note has the reset SQL to run first.
Evidence:
`docs/internal/evidence/7142-supply-chain-impact-fencing-token.md`.
- Types: `SupplyChainImpactFinding`, `SupplyChainImpactFactFilter`,
  `SupplyChainImpactWrite`, `SupplyChainImpactWriteResult`,
  `SupplyChainSuppressionDecision`, `SupplyChainSuppressionState`,
  `GoVulnerabilityFinding`, `JVMReachabilityFactFilter`.

## Dependency rule

One-way imports only: this package reads the shared tier (`contract`,
`factload`, `factdecode`, `factwrite`, `schemadecode`, `crossscope`,
`payloadcore`, `supplychainmodel`), the already-extracted `cicdrun`,
`containerimage`, `servicecatalog`, `correlation`, `source`, and
`securityalert` subpackages (exported consts, fact kinds, and
alert/consumption types only), plus `facts`, `packageidentity`,
`telemetry`, `truth`, `environment`, and the SDK `factschema`. It never
imports the parent reducer package. The parent references it only through
`supplychaincore`-qualified names; external callers keep their `reducer.X`
spelling through the supply_chain_impact stanza in the parent's
`compat_correlation.go`.
