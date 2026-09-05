# Shared-machine Go execution

- MUST isolate the Go build cache per worktree when running parallel agents.
  Set `GOCACHE=<worktree-path>/.gocache` in each agent's environment before
  any `go build`, `go test`, or `golangci-lint` invocation. Parallel agents
  sharing the default `~/.cache/go-build` corrupt each other's incremental
  builds and can wipe in-progress compilation for a sibling agent.
- MUST NOT run `go env -w` on a shared machine. That file
  (`$(go env GOENV)`) is read by every concurrent `go` invocation, so a write
  lands inside whatever gate another session is running, and a run that reads
  it twice can see two configurations. Export the variable in your own shell
  instead; announce it first if you must write the file.
- MUST keep `GOTMPDIR` short and outside every worktree, or leave it unset.
  macOS caps a unix-socket path at 104 bytes, so a long `GOTMPDIR` makes
  `t.TempDir()` hand socket-binding tests a path they cannot bind — it presents
  as an ordinary red that a quiet re-run will not fix. A worktree-internal one
  inverts git-dependent tests, so the two constraints pull opposite ways.
- MUST use distinct `GOCACHE` directories for concurrent Go commands launched
  inside the same worktree, or run those commands serially. Sharing one
  worktree-local cache across simultaneous `go test`, `go run`, verifier, or
  lint commands can produce missing cache object, vet, or linker failures that
  look like product regressions.
