# Recorded scanner-worker output for eshu-hq/eshu#10030

Real `go_reachability` analyzer output over the edited orders-api fixture,
recorded 2026-11-20. The cassette's orders-api Go facts are transcribed from
`emitted.json`; the harness source is kept as `harness-main.go.txt`.

- Harness command (run from `eshu-scanner-worker` at `f663bd0`):
  `go run ./go/tmp-record10030 <eshu>@drive/10030-golden-record:tests/fixtures/ecosystems/orders-api`
  `/tmp/spike10030/proxy /tmp/spike10030/vulndb /tmp/spike10030/seed <out>`
  (`harness-main.go.txt` is the exact source; the throwaway copy under
  `go/tmp-record10030` is deleted after recording).
- Scanner-worker commit: `f663bd0` (main; `govulncheck` stage B from #51).
- Toolchain: child `go version go1.26.11 linux/arm64`
  (`PinnedToolchainVersion()` of the harness binary; seed `VERSION`
  `go1.26.11`); host `go1.26.6` never touches the scan.
- Scanner: `govulncheck@v1.9.0` (`Scanner: govulncheck@v1.9.0`).
- Staged HEAD (bare remote `orders-api.git`, golden_corpus_git recipe:
  Golden Gate identity, fixed dates, message `initial`):
  `bbca297156091bebc27cd79940f9f9b6017914bb`.
- Module proxy (`GOPROXY=file://...`, `GOSUMDB=off`):
  `github.com/acme/lib-common v1.0.0` (fixture `common.go` byte-identical,
  with the `Uncalled` entry point) and `github.com/acme/legacy-lib v0.1.0`.
- Vulnerability DB (`GOVULNDB=file://...`, flat OSV dir): `GO-2026-9229`
  (`Identity`, alias `CVE-2026-9229`), `GO-2026-9301` (`Uncalled`, alias
  `CVE-2026-9301`), `GO-2026-9401` (`LegacyBoom` on legacy-lib, alias
  `CVE-2026-9401`); all fixed in `1.0.1`/`0.1.1`.
- Result: 5 facts (2 module evidence, 1 symbol + 1 not_called reachability,
  1 `toolchain_unsupported` warning for `legacy/go.mod`), figures
  `findings_symbol:1 findings_not_called:1 modules_analyzed:1
  modules_total:2`, labels `scanner_version:v1.9.0 toolchain:go1.26.11
  vulndb_last_modified:2026-06-24T00:00:00Z`.
- Determinism: two consecutive runs are byte-identical (`diff` clean),
  including stable keys, trace positions (`main.go:11:18`,
  `common.go:9:6`), and `target_locator_hash`.
