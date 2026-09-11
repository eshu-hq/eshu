# internal/reducer/intents/shared

## Purpose

Namespace-only parent directory. It holds no Go declarations of its own —
only `doc.go`'s package comment — and exists to group the shared-projection
intent substrate (`intents/shared/worker`) under the name issue #6061's
owner-approved target tree gives it.

## Ownership boundary

**Owns:** nothing. **Contains:** `intents/shared/worker`.

## Exported surface

None.

## Dependencies

None beyond what `intents/shared/worker` imports.

## Related docs

- `go/internal/reducer/intents/README.md`
- `go/internal/reducer/intents/shared/worker/README.md`
- `docs/internal/design/package-restructure.md` — the #6061 restructure
