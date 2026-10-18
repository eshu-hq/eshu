# AGENTS.md — go/internal/collector/repo/git/resolve

## Read first

1. `go/internal/collector/repo/git/resolve/README.md` — surface and signals
2. `go/internal/collector/repo/git/resolve/doc.go` — the package contract
3. `go/internal/collector/repo/git/deadline/README.md` — the retry loop whose
   `RefreshRequester` hook this cache satisfies

## Rules

- This package is a leaf. `git` imports it; it must never import `git` or
  `deadline` (`RequestRefresh` satisfies the hook structurally).
- One receiver goroutine runs every lookup. Keep it that way: a lookup started
  anywhere else breaks the single-flight guarantee the concurrency proof
  measures.
- Never store an empty or failed answer. A failed refresh keeps the last good
  set; an address set is replaced only whole, by the latest successful lookup,
  never merged with older addresses.
- Never block a caller on the request send, and never let a first command wait
  past the lookup deadline for its pin — it runs unpinned instead
  (`reason=cache_empty`).
- Never pin under a proxy setting, for an IP-literal host, or with the kill
  switch off. SSH remotes never reach this package; keep that gate in the
  caller.
- The label sets are closed (`result`: success, error, empty; request
  `reason`: initial, failure, periodic, suppressed; fallback `reason`:
  cache_empty, proxy_set). Add a value here, in the telemetry instruments, and
  in the metrics docs together.
