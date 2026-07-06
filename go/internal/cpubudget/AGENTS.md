# AGENTS.md — internal/cpubudget guidance for LLM assistants

## Read first

1. `go/internal/cpubudget/README.md` — purpose, exported surface, invariants
2. `go/internal/cpubudget/cpubudget.go` — `UsableCPUs`, a one-line wrapper
   over `runtime.GOMAXPROCS(0)`
3. `go/internal/cpubudget/cpubudget_test.go` —
   `TestGoDirectiveSupportsAutomaticGOMAXPROCS`, the go.mod-version guard this
   package's entire design depends on
4. `go/internal/runtime/memlimit.go` — the memory-side sibling
   (`ConfigureMemoryLimit` / `readCgroupMemoryLimit`), which still needs a
   handwritten cgroup reader because memory has no Go-runtime-native
   cgroup-awareness equivalent

## Invariants this package enforces

- **Zero internal dependencies.** `cpubudget.go` imports only `runtime`.
  This is not a style preference — it is why the package exists. Adding any
  `github.com/eshu-hq/eshu/go/internal/...` import here risks recreating the
  import cycle this package was extracted to break (see README "Gotchas").
  If a change seems to need one, stop and reconsider the design instead.
- **This is a thin wrapper, not a cgroup reader.** `UsableCPUs()` does not
  read `/sys/fs/cgroup/...` itself. It relies entirely on the Go 1.25+
  runtime's automatic `containermaxprocs` GODEBUG (default-on) having
  already set `GOMAXPROCS` from the container's cgroup CPU quota before any
  Eshu code runs. Do not add a manual `ConfigureGOMAXPROCS`-style call or a
  handwritten cgroup-quota parser back into this package — that was tried
  and reverted because it reinvented what the runtime already does, and it
  left call sites unrouted where the reinvented setup call was never wired
  in (the exact failure mode that led to this simplification).
- **The go.mod version guard is load-bearing.**
  `TestGoDirectiveSupportsAutomaticGOMAXPROCS` asserts this module's `go`
  directive in `go.mod` is `>= 1.25`. This is not a routine compatibility
  check — if it ever fails, `UsableCPUs()`'s entire cgroup-awareness
  assumption is void, and every worker-count default silently reverts to
  reporting the host CPU count in cgroup-limited containers.
- **`UsableCPUs()` is the sanctioned replacement for `runtime.NumCPU()` in
  worker-count defaults OUTSIDE `internal/parser`.** Every default worker
  count in the codebase should call `cpubudget.UsableCPUs()`, not
  `runtime.NumCPU()` directly, unless the use is genuinely host-CPU-correct
  (e.g. GC tuning knobs unrelated to worker fan-out) — with one deliberate,
  temporary exception: `internal/parser`'s two worker-sizing sites
  (`interproc/solve.go`, `go_package_interface_prescan.go`) are **not**
  routed and remain on `runtime.NumCPU()` / `runtime.GOMAXPROCS(0)` directly.
  This is not an import-cycle issue — see "Deferred: internal/parser" in
  README.md for the actual reason (the parser-relationship-kit gate) and
  treat it as scoped-out, not forgotten.

## Common changes and how to scope them

- **Route a new worker-count default through this package** — replace
  `runtime.NumCPU()` (or `runtime.NumCPU` stored as a function value) with
  `cpubudget.UsableCPUs()` (or `cpubudget.UsableCPUs` as a function value) at
  the call site, preserve every existing clamp exactly, and add the import
  `github.com/eshu-hq/eshu/go/internal/cpubudget`. Before doing this, check
  whether the target package (or anything importing `internal/runtime`)
  creates a cycle — see the README's "Gotchas" section for the exact chain
  that motivated this package's existence. If the target site is under
  `go/internal/parser/*.go`, expect the parser-relationship-kit gate to
  require a parser test change and a language-support doc update in
  lockstep — plan for that gate rather than being surprised by it (see
  README "Deferred: internal/parser" for the two sites already known to be
  affected).
- **Bumping or downgrading the `go` directive in `go.mod`** — if downgrading
  below 1.25, `TestGoDirectiveSupportsAutomaticGOMAXPROCS` will fail on
  purpose. Do not silence it; either stay at 1.25+, or reintroduce a
  handwritten cgroup-quota reader in this package (the git history before
  this simplification has one, mirroring `internal/runtime/memlimit.go`'s
  style) before downgrading.

## Failure modes and how to debug

- Symptom: worker pools over-spawn inside a CPU-limited container → cause:
  a worker-count default still calls `runtime.NumCPU()` (or stores
  `runtime.NumCPU` as a function value) directly instead of
  `cpubudget.UsableCPUs()` → fix: grep
  `rg 'runtime\.NumCPU' go/cmd go/internal -g '*.go' | rg -v _test | rg -v internal/parser`
  for stragglers (matches with or without parens — some sites store the bare
  function value, e.g. `cmd/eshu/local_host.go`'s `localHostNumCPU` var). This
  should be empty for everything **outside** `internal/parser` — its two
  sites are a known, deliberate exception (see "Deferred: internal/parser" in
  README.md), not a straggler to fix.
- Symptom: `TestGoDirectiveSupportsAutomaticGOMAXPROCS` fails → cause: the
  `go` directive in `go/go.mod` was downgraded below 1.25 → fix: do not
  silence the test; either revert the downgrade or reintroduce a
  handwritten cgroup-quota reader (see "Common changes" above).
- Symptom: `go vet`/`go test` on `internal/runtime` (or any of
  `internal/collector`, `internal/parser`, `internal/reducer`) reports an
  import cycle after adding a `cpubudget` (or `internal/runtime`) import →
  cause: something in the new import path re-imports the package that
  started the change → fix: trace the chain with
  `go vet ./internal/runtime/ ./cmd/...` (it prints the full cycle), and if
  the cycle runs through `internal/runtime` specifically, route through
  `cpubudget` instead — that is exactly the case this package was built to
  solve.

## Anti-patterns specific to this package

- **Adding an internal import to `cpubudget.go`.** The whole point of this
  package is that it has none. If a change needs one, the change belongs
  somewhere else, or the dependency needs to move to this leaf too.
- **Reinventing cgroup reading here.** A handwritten cgroup v1/v2 CPU-quota
  parser lived in this package once and was deliberately removed — Go
  1.25+'s automatic GOMAXPROCS already does that job. Reintroducing it
  without first proving the go-directive guard test is failing (i.e. the
  module dropped below Go 1.25) is unnecessary duplication of what the
  toolchain provides for free.
- **Adding a manual "configure" call back to `main()`.** There is
  intentionally no `ConfigureGOMAXPROCS`-style function to call at startup.
  `UsableCPUs()` needs no setup — `GOMAXPROCS` is already correct by the
  time `main()` runs. A prior version of this package had such a call, and
  it was a partial fix: the manual setup call left some worker-sizing call
  sites (e.g. `cmd/reducer`, `cmd/eshu`) unrouted, and callers wrongly
  assumed "call `ConfigureGOMAXPROCS`" and "route to `UsableCPUs()`" were the
  same fix when they are not — the routing is the fix.
