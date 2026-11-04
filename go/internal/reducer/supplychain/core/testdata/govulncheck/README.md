# Recorded govulncheck output

Recorded for eshu-hq/eshu#10028 (epic #10002). Test input only.

- Tool: `govulncheck@v1.9.0`, Go `go1.26.6` (linux/arm64), DB
  `https://vuln.go.dev` last modified `2026-11-02T22:01:43Z`.
- Module: `example.com/gvcexp` requiring `golang.org/x/text v0.3.5`; `main`
  calls `language.Parse` and never calls `language.ParseAcceptLanguage`.

| File | Command | Notes |
| --- | --- | --- |
| `xtext_v0_3_5_symbol_source.jsonl` | `govulncheck -json -mode source ./...` (exit 0) | All 27 `finding` messages, `config`, `SBOM`, and both `progress` messages kept. 188 of 192 `osv` messages dropped (the four x/text advisories kept). |
| `xtext_v0_3_5_package_source.jsonl` | `govulncheck -scan package -json ./...` (exit 0) | Only the x/text `finding` messages (stdlib findings dropped) and the two matching `osv` messages. |
| `toolchain_unsupported_config_only.jsonl` | same module with `go 1.99`, `GOTOOLCHAIN=local` (exit 1) | The only stdout govulncheck wrote: the `config` message. |
| `toolchain_unsupported.stderr.txt` | same run | stderr of the failed run. |

Messages are re-serialized one per line with key order and values unchanged;
govulncheck itself prints indented JSON.
