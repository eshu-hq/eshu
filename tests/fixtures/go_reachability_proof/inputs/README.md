# Proof scan inputs for the #10031 live run

The fixture's dependencies are synthetic (`github.com/acme/*` is not on the
public module proxy), and the recorded advisory set must stay fixed for the
verifier's exact pins. This directory carries the same inputs the #10030
cassette was recorded with:

- `proxy/` — a `GOPROXY=file://` directory: `lib-common v1.0.0`
  (`Identity` + `Uncalled` entry points) and `legacy-lib v0.1.0`
  (`LegacyBoom`). Built from `src/legacy-lib/` and the committed
  `tests/fixtures/ecosystems/lib-common` Go sources with fixed zip
  mtimes, so the bytes are reproducible.
- `vulndb/` — the recorded local advisory set (`GOVULNDB=file://`):
  `GO-2026-9229` (`Identity`), `GO-2026-9301` (`Uncalled`), `GO-2026-9401`
  (`LegacyBoom` on legacy-lib), all fixed in `1.0.1`/`0.1.1`.
- `src/legacy-lib/` — the legacy-lib module sources (`go.mod`,
  `legacy.go`). The lib-common Go sources live in
  `tests/fixtures/ecosystems/lib-common`; the zips above embed them.

The proof overlay seeds this directory into the shared `eshu_data` volume
(`scan-inputs-seed` service) so worker recreations never lose it, and the
worker resolves `GOPROXY`/`GOSUMDB=off`/`GOVULNDB` at those volume paths.
All content is IP-free fixture material: no proprietary logic.
