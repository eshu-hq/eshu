# OSV identity edges: shared golden, mirror test, and edge counter

Item 3.1 of the advisory-intelligence epic (#9209, issue #9954). This note
records what shipped in this repository, how it was proved, how it rolls out,
and what was not checked. The design context is in
[the advisory-intelligence phases](../design/advisory-intelligence-phases.md).

## What changed

The OSV `vulnerability.cve` payload now carries its raw identity edges in a
form later reducer work can read without guessing:

- `aliases`, `related`, and the new directed `upstream` are each trimmed,
  de-duplicated, and sorted. `aliases` and `related` are always written (an
  empty list is JSON `null`); `upstream` is written only when it has an id.
- `upstream` is never added to `aliases` or `correlation_anchors` and never
  selects `cve_id`. A distro bundle of CVEs is `upstream`, not `aliases`.
- `status` is `withdrawn` only for a record with a non-blank `withdrawn`
  timestamp; an active record has no `status` key.
- One shared golden pins that shape for core and the extracted collector:
  `sdk/go/factschema/fixturepack/payloads/osv_identity_edges_golden.json`
  (accessor `OSVIdentityEdgesGolden()`, seven cases). The fixturepack test pins
  its digest and derives every expected field from each case record; the
  in-tree collector replays every case through `OSVRecordEnvelopes`.
- The extraction-prep mirror test asserts the same edge shape for one record.
- New counter `eshu_dp_vulnerability_intelligence_identity_edges_emitted_total`
  (`source`, `edge_kind` of `aliases|related|upstream`, `status` of
  `active|withdrawn`). It counts edge elements, not facts, and is emitted in
  `vulnruntime` beside the fact counter. A raw NVD `vulnStatus` is never a
  label: only a payload `status` of `withdrawn` is withdrawn. `vulnruntime` has
  four sources (`rg -n 'Source = ' go/internal/collector/vulnerabilityintelligence/vulnruntime/source.go`)
  and only OSV writes these keys, so at most 4 x 3 x 2 series and in practice
  six; a record's edges are tens, so cost per target is at most six `Add` calls.

Golden digest (SHA-256): `3df283f40ecbe08dcd559bd3e3500c865411d247d8e644383c52fda8b3266010`.

## Proof

Red before green: the counter tests failed to build until `recordIdentityEdges`
existed; the golden replay failed (`payload["upstream"]` order) when the golden
was seeded with an unsorted upstream and passed again after restoring it; the
mirror test failed when `upstream` was seeded into `correlation_anchors`.

| Command | Exit |
| --- | --- |
| `cd go && go vet ./internal/collector/vulnerabilityintelligence/... ./internal/telemetry/...` | 0 |
| `cd go && go test ./internal/collector/vulnerabilityintelligence/... ./internal/telemetry/... ./cmd/collector-vulnerability-intelligence/... -count=1` | 0 |
| `cd sdk/go/factschema && go test ./... -count=1` | 0 |
| `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh` | 0 |
| `bash scripts/verify-package-docs.sh` | 0 |
| `mkdocs build --strict --config-file docs/mkdocs.yml` | 0 |
| `gofmt -l` on touched trees; `git diff --check` | clean; 0 |
| `cd go && go test ./internal/collector/vulnerabilityintelligence/ -run '^$' -bench OSVRecordEnvelopesUpstream -benchtime 20000x -count=3` | 0 |
| `cd go && go test ./internal/collector/vulnerabilityintelligence/vulnruntime/ -run '^$' -bench RecordIdentityEdges -benchtime 2000x -count=1` | 0 |

Golden corpus: no cassette or B-7 snapshot change. The cassette
`testdata/cassettes/vulnerabilityintelligence/supply-chain-demo.json` is
replayed verbatim and does not pass through `OSVRecordEnvelopes`; it has no
`upstream` key (`rg -n upstream testdata/cassettes/vulnerabilityintelligence/`
finds nothing, and the only snapshot hit is the unrelated
`upstream_dependencies`). No reducer reads `upstream` (`rg '"upstream"' go` outside the
collector finds only an unrelated graph-capture allowlist), and the cassette
facts are unchanged, so projected graph truth is unchanged and the B-7 gate was not rerun.

## Performance and observability

Performance Evidence: no graph, Postgres, queue, or lease path is touched; the
two touched paths are in-process. Host: linux/amd64, 12 threads, go1.26.10,
`-benchtime 20000x -count=3` (envelope) and `2000x` (counter). The baseline for
the envelope is the record with no `upstream` (`upstream_0`, the shape before
#9954). Record shape: one alias, one related id, plus the listed upstream ids.

| Record conversion (`BenchmarkOSVRecordEnvelopesUpstream`) | ns/op | allocs/op |
| --- | --- | --- |
| baseline, 0 upstream ids | about 6,900 to 7,050 | 68 |
| 3 upstream ids | about 6,960 to 7,090 | 70 |
| 15 upstream ids (5x) | about 7,950 to 8,070 | 73 |
| 30 upstream ids (10x) | about 8,925 to 9,170 | 73 |

| Counter, 1000 facts per target (`BenchmarkRecordIdentityEdges`) | ns/op | allocs/op |
| --- | --- | --- |
| 3 ids per kind (1x) | 120,048 | 12 |
| 15 ids per kind (5x) | 123,108 | 12 |
| 30 ids per kind (10x) | 118,049 | 12 |

No-Regression Evidence: a record with no `upstream` pays one `cleanStrings` of
a nil slice, which allocates nothing (`upstream_0` is 68 allocs and writes no
`upstream` key). Cost grows with the record's own upstream list, by about
2,000 ns for 30 ids, against a fetch that is an HTTP round trip. The counter
costs about 120 ns per fact (about 120 microseconds per 1000-fact target), flat
in edge count, with 12 allocs per target for six `Add` calls. Edges per OSV
record are tens, so 1x, 5x, and 10x all sit inside the bounded range above.

Observability Evidence: an operator sees how many identity edges each source
wrote, split by `edge_kind` and `status`, on
`eshu_dp_vulnerability_intelligence_identity_edges_emitted_total`. A rising
`upstream` series with a flat `aliases` series is a distro feed arriving; a
`withdrawn` series is retractions arriving. The counter has no per-id label, so
no advisory or CVE id reaches a dashboard. The coverage row is in
`docs/public/observability/telemetry-coverage-collectors.md`.

## Rollout

- No registry or `schema_version` change: `vulnerability.cve` stays 1.0.0 and
  the payload-usage manifest is untouched. The new key is additive raw payload;
  no typed decoder reads it yet.
- `sdk/go/factschema/vulnerability/v1/cve.go` and its schema JSON are not
  edited. The in-tree mirror is synced byte for byte from a published eshu-sdk
  tag, and the typed `aliases`/`related`/`upstream` fields land in a separate
  eshu-sdk PR (rc.14, owner-dispatched). The extracted collector then pins that
  tag and replays the same golden bytes through `fixturepack`.
- No data migration. A record without `upstream` produces an unchanged payload.
  Older readers ignore the extra key.

## NOT_CHECKED

- The B-7 golden-corpus gate (`scripts/verify-golden-corpus-gate.sh`): emission
  is unchanged for every cassette record, so it was not run.
- `make pre-push` and `make pre-pr`: left to the coordinator.
- The extracted collector and eshu-sdk repositories: not touched or run here.
- Counter values on a live collector or in the QA environment.
