# internal/reducer/intents

## Purpose

Namespace-only parent directory. It holds no Go declarations of its own —
only `doc.go`'s package comment — and exists to group the reducer's
intent-processing subpackage tree under the names issue #6061's owner-approved
target tree gives them.

## Ownership boundary

**Owns:** nothing. **Contains:** `intents/shared` (the shared-projection
intent substrate: `intents/shared/worker`) and `intents/phase` (graph-projection
phase repair: `intents/phase/repair`).

## Exported surface

None.

## Dependencies

None beyond what its child packages import. This directory's own `doc.go`
imports nothing.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/intents/shared/README.md`
- `go/internal/reducer/intents/phase/README.md`
- `docs/internal/design/package-restructure.md` — the #6061 restructure
