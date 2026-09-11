# internal/reducer/intents/phase

## Purpose

Namespace-only parent directory. It holds no Go declarations of its own —
only `doc.go`'s package comment — and exists to group graph-projection phase
repair (`intents/phase/repair`) under the name issue #6061's owner-approved
target tree gives it.

## Ownership boundary

**Owns:** nothing. **Contains:** `intents/phase/repair`.

## Exported surface

None.

## Dependencies

None beyond what `intents/phase/repair` imports.

## Related docs

- `go/internal/reducer/intents/README.md`
- `go/internal/reducer/intents/phase/repair/README.md`
- `docs/internal/design/package-restructure.md` — the #6061 restructure
