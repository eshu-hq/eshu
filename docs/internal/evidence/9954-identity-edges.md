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
  label: only a payload `status` of `withdrawn` is withdrawn. At most 5 sources
  x 3 kinds x 2 statuses series; a record's edges are tens, so cost per target
  is at most six `Add` calls.

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

Golden corpus: no cassette or B-7 snapshot change. The cassette
`testdata/cassettes/vulnerabilityintelligence/supply-chain-demo.json` is
replayed verbatim and does not pass through `OSVRecordEnvelopes`; it has no
`upstream` key (`rg -n upstream testdata/cassettes/vulnerabilityintelligence/`
finds nothing, and the only snapshot hit is the unrelated
`upstream_dependencies`). The edge keys were already read by no reducer, so
projected graph truth is unchanged and the B-7 gate was not rerun.

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
