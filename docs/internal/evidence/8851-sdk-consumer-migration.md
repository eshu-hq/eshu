# SDK consumer migration no-regression evidence (#8851)

Mechanical migration of all in-repo importers from
`github.com/eshu-hq/eshu/sdk/go/{collector,factschema}` to the standalone
`github.com/eshu-hq/eshu-sdk/{collector,factschema}` modules. Branch
`drive/8851-sdk-consumer-migration`, base `e39c3bb949` (rebased; prior base `8a66731df3`).

No-Regression Evidence (#8851): the migration diff is import-path-only.
`git diff e39c3bb949..HEAD -- go/` holds 1264 added/removed content lines (identical count after the rebase);
1254 swap the old module path for the new one and nothing else. The other 10
are the planned scaffold-template updates (`component_init_test.go` drops the
directory replace, `scaffold_templates.go` pins the new path and proxy
resolution) plus blank lines from import regrouping. No assertion, branch,
query, schema, or wire byte changes. The factschema sources linked at rc.4
were reviewed byte-faithful against core `62b925d86b`, so the reducer links
identical code through a new module path; observed graph truth cannot differ.

No-Regression Evidence (tests, #8851): `go build ./...` green on the migrated
tree. Changed-package `go test` (105 packages): green except two to three
`collector/repo/git` TempDir-cleanup flakes (`RemoveAll .../.git: directory
not empty`, test bodies pass; the count moves between runs, same family),
reproduced identically on the unmodified pre-rebase base
in a disposable worktree —
pre-existing host load behavior, not the migration. `go test -race` on the
changed packages passes (171s). `gofumpt`, `golint`, `go vet`, and the
merge-tree vet pass. Pins: `collector v0.1.0-rc.3`, `factschema v0.1.0-rc.4`
(both proxy Origin-hash and sumdb verified); zero SDK `replace` directives
(the two remaining replaces are pre-existing tree-sitter/embedded-postgres
pins). Host note: tree-sitter perl cgo needs `CC=clang` under host gcc 16;
CI runners are unaffected. `go.mod` declares `go 1.26.10`; proof ran under `GOTOOLCHAIN=go1.26.10` (`go version go1.26.10 linux/amd64`).

No-Observability-Change (#8851): no metric, span, log key, queue table,
worker, lease, or status shape is added, removed, or renamed. Operators keep
the existing reducer spans, execution counters, and `eshu_dp_reducer_*`
metrics; the migration changes where identical sources are imported from,
not what executes.
