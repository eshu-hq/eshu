# Dead-Code OpenAPI Path Fragments

The OpenAPI path fragments for the dead-code routes: single-repository
investigation, per-repository scan, and cross-repository classification.

Layout:

- `investigation.go` — `Investigation`: `POST /api/v0/code/dead-code/investigate`,
  a bounded dead-code investigation packet.
- `scan.go` — `Scan`: `POST /api/v0/code/dead-code`, the per-repository
  dead-code scan.
- `cross_repo.go` — `CrossRepo`: `POST /api/v0/code/dead-code/cross-repo`,
  cross-repository dead-code classification against consumer evidence.

`openapi/spec.go` concatenates all three directly, alongside the parent
`code` package's own fragments — there is no aggregate constant here.

## Move evidence

`Investigation` and `CrossRepo` moved here verbatim from the parent
`code` package's single `dead.go` (which held both, a deliberate one-file
exception the parent's own docs called out), and `Scan` from `dead_scan.go`
(Issue #6060 lane C, #6648). The owner then split the exception: three
exported constants repeating the parent package word mid-name
(`code.DeadCodeInvestigation`, `code.DeadCodeScan`, `code.CrossRepoDeadCode`)
nest as their own leaf, dropping the `DeadCode`/`Dead` prefix on each name
since the package itself now says `dead`. Only the package clause, file
names, and constant names changed; the JSON each constant renders is
unchanged.
