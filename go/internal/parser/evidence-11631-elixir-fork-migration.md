# Evidence: Elixir grammar migration to eshu-hq fork (eshu#11631)

The root module drops the dead
`github.com/tree-sitter/tree-sitter-elixir v0.0.0-00010101000000-000000000000`
require and the `replace => github.com/elixir-lang/tree-sitter-elixir v0.3.5`
(which never propagated to nested `go/coreserve`, since upstream's go.mod still
declares the old module path). It requires
`github.com/eshu-hq/tree-sitter-elixir v0.3.5-eshu.1` verbatim and migrates the
4 Go import sites (`internal/parser/elixir/language.go`,
`internal/parser/elixir/runtime_evidence.go`, `internal/parser/runtime.go`,
`internal/parser/fingerprint/comment_invariance_test.go`) with the same local
alias and `/bindings/go` subpath. No logic, signature, or call-site change.

No-Regression Evidence: import-path-only rename; the fork diff vs upstream
v0.3.5 is the go.mod module line plus the bindings self-import (zero grammar/C
byte changes), so no benchmark delta is possible. `go build ./...` and
`go vet ./...` exit 0; `go test -count=1 ./internal/parser/...`: 61 packages
ok, 0 FAIL; `go list -m all` shows only the fork; `go mod verify` passes.

No-Observability-Change: no metric, span, log, or telemetry touched.
