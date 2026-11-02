# 6228 secgroup-pair + s3-grant determinism leaf — live evidence

Refs #6228. Branch `drive/6228-secgroup-s3grant` off `3e07d26b1f`.
Families: `security_group_sg_rule`, `security_group_rule_endpoint`
(ONE shared cassette), `s3_external_principal_grant`.

## Matrix result

`bash scripts/verify-ifa-determinism.sh --keep`, exit 0, full gate:

- `PASS: graph-determinism matrix green across N=1 2 4`
  (project `eshu-ifa-det-secgroup`, postgres:15636, neo4j-bolt:7793).
- Digests byte-identical across workers:
  `N=1 digest=30b3e721d00fdba26610f71c666ab8f62ac06ccece28ab0b666051845701bf9a wall=280s`,
  `N=2 ... wall=188s`, `N=4 ... wall=214s`.
- All three families exact-set green at N=1, N=2, N=4 AND in each
  post-delta chain (sg 5/5 ALLOWS_*, endpoint 5/5 TO, s3grant 4/4
  GRANTS_ACCESS_TO); 215 `matched exactly` lines gate-wide.

## Incident found live: TO fixture over-asserted `target_label`

The first N=1 run failed at the endpoint assert (0/5 TO in graph) while
the SG assert's 5 edges were present. Forensics on the kept stack:

- All three `security_group_*` domains `succeeded`; node census showed
  5 rule nodes, 2 CidrBlock (one with the exact expected uid), 1
  PrefixList; PG payloads intact; extract replayed over live-loaded
  envelopes yields nodes=5, sg=5, to=5.
- The TO edges existed with correct endpoints but carry no
  `target_label` edge property — production never sets one (writer SETs
  scope/generation/evidence only; no sibling writer duplicates a target
  node label onto the rel either).
- Fix (this branch): drop `target_label` from the 5 TO fixture entries
  and from the offline row→expected conversion (extract rows keep the
  field — the writer needs it for MATCH grouping). No coverage lost:
  the writer's `MATCH (target:LABEL)` enforces label agreement at write
  time, so a wrong-label regression no-ops the write and fails as
  missing. Re-proved live: endpoint assert 5/5 on the same graph;
  `go test ./internal/ifa/...` green.
- Pre-existing inconsistency (fixture predates this lane; offline guard
  tests extract rows, never graph properties, so only a live run could
  catch it). s3grant fixture audited clean (asserts only writer-SET
  properties).

## Queued next

Fault trio live: shard 6 (s3grant), shard 7 (sg_rule), shard 8
(rule_endpoint) of `verify-ifa-fault-injection.sh --shard k/8`.
On full green: waiver→coverage rows + roster LiveProven flips +
trigger stems, then final review and commit(s).

## Post-run value scrub

After the live runs the cassettes' synthetic identifiers were scrubbed to
the cassette-author allowlist (`10.0.0.0/8` → `192.0.2.0/24`; external
accounts to repdigit documentation values; S3 service principal to its
hyphenated form). Digests above are the pre-scrub runs'; static mirrors
re-pass on the scrubbed values and the PR wave re-proves live.

## Push-gate no-regression record

No-Regression Evidence: baseline = pre-scrub tree (live determinism drive
above: N=1/2/4 walls 280/188/214s, digest `30b3e721…`; `go test
./internal/ifa/...` green; fault shards 6/7/8 exit 0). After = scrubbed tree
on head `4f7097fbaa`: `go test ./internal/ifa/...` all green,
`go vet ./internal/ifa/...` clean, fault + determinism static mirrors pass,
cassette-author exit 0 over 94 files. Backend: the gate's standard live
determinism Compose stack (project `eshu-ifa-det-secgroup`, same stack both
runs); toolchain go1.26.6 linux/arm64, same host both runs. Input shape: 3
families, 115 fault cells, 5/5/4 exact edge asserts — identical counts both
runs. Why safe: the diff touches string literals only (identifier values,
value-derived uid hashes, comments) — no control flow, query text,
concurrency, schema, or batching change; the exact-match edge comparator
re-passing proves end-to-end equivalence.

No-Observability-Change: no metric, span, log line, or status surface
touched; the EXIT-trap cleanup/dump paths are byte-identical (moved file,
not modified).
