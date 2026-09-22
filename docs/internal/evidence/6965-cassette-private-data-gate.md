# #6965 Phase 2: a private-data gate over testdata/cassettes

## What it guards

`scripts/verify-cassette-author.sh` validated cassette format only. It never
inspected values, so a recorded cassette carrying a real IP, account ID, ARN,
hostname or organisation name would have passed every gate. #6965 Phase 3
records cassettes from a live cluster, and a leak into a committed cassette is
permanent in git history, so this gate lands first.

The scan lives in `scripts/lib/cassette_private_data_pattern.sh`: seven
alternatives (`ipv4`, `nodeip`, `ipv6`, `account12`, `arn`, `hostname`,
`identifier`). Each match is a candidate that passes only through that
alternative's anchored allowlist of documentation values, so an unlisted value
fails. Findings print `file:line:alternative`, never the value.

## Proof

Seeded violations, `bash scripts/test-verify-cassette-author.sh` (bash 5.3),
exit 0:

- RED: 15 planted values, each in its own scratch corpus, each exit 1 naming
  `planted.json:1:<alternative>` with the value absent from the output. The
  plants cover every alternative, an in-cluster `svc.cluster.local` name, a
  short `.svc` name, a `.co.uk` domain, a wildcard host, `.corp` and `.consul`
  hosts, an address ending a sentence, an EKS node name and a real MAC.
- Mutations of a scratch copy of the library, 16 in all, each red for its
  asserted reason: delete each alternative, make each alternative never match,
  widen the hostname allow to `.*`, delete a planted sample.
- A caller without `pipefail` still gets rg's own exit status; an uncompilable
  pattern fails with `rg exit 2`.
- GREEN: the real gate over the committed tree, `68 file(s) scanned`.

Independent review found fail-open shapes (a wildcard `.local` allow, wildcard
hosts, missing enterprise TLDs, an address ending a sentence, hyphenated node
IPs, a probe pipe that lost rg's status). Each was fixed with a RED plant
before the gate was pushed.

## Stated limits

Hostname detection is lexical: a domain under a TLD outside the gate's list is
not a candidate. Only colon-separated MACs are candidates. In CI the
identifier alternative runs with the committed canary only, because the
identifier list never lives in git; #6979 tracks a repository secret for it.
Phase 3 therefore also requires two further independent validations of every
recording.

## Runtime impact

The Go changes are two entries in `go/internal/envregistry/entries.go`
(`ESHU_PRIVATE_IDENTIFIERS_FILE`, `ESHU_PRIVATE_IDENTIFIERS_REQUIRED`) and the
`scriptWorkflowSoundSubsetCount` constant in `go/internal/cigates`. No runtime
binary reads either variable; the registry entries exist so that the public
docs citing them have an owner (`docs-cli-env-refs`).

No-Regression Evidence: no runtime code path changes. The two registry entries are metadata read by the env-registry doc generator and the docs-cli-env-refs checker, and only `scripts/lib/cassette_private_data_pattern.sh` reads the variables. `go test ./internal/envregistry ./internal/cigates -count=1` passes, and `bash scripts/verify-docs-cli-env-refs.sh` reports `OK: 1818 reference(s) checked`. The gate runs in the pre-push floor over 68 files, not in any runtime stage.

No-Observability-Change: nothing in the ingester, reducer, API or MCP runtime changes, so there is no new metric, span or log key. The gate reports `cassette private-data scan: N file(s) scanned`, the loaded identifier count, and `file:line:alternative` findings on its own output.
