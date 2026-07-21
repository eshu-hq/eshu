# Scala Parser Agent Notes

Read `language.go` first, then `dead_code_roots.go`. Keep this package
parent-independent: use `internal/parser/shared` for payload, source, sorting,
and tree-sitter node helpers. Do not import `internal/parser`.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.

Dead-code roots must stay syntax-backed. Do not root broad public Scala API
surfaces, all controller methods, implicits/givens, macros, Play route-file
entries, or compiler-plugin output without a dedicated parser/query design and
fixture coverage.

`dogfood_real_repo_test.go` is a standing regression test (#5399) backing the
`real-repo-validated` grade; it is not opt-in like the `SCALA_PARSE_DUMP`
equivalence harness. Do not hand-edit
`testdata/dogfood_real_repo_snapshot.txt`; regenerate it with
`DOGFOOD_UPDATE_SNAPSHOT=1 bash scripts/dogfood-scala.sh` after an intended
parser change and verify the bucket-count delta is expected.
