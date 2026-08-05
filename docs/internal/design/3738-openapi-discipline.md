# X2: OpenAPI surface discipline

**ADR**: docs/internal/design/3738-openapi-discipline.md
**Epic**: #3738
**Leaf issues**: #3781 (verifier), #3782 (regenerator, optional), #3783 (CI gate)

## Problem

The Eshu query API serves 200+ HTTP routes registered via `mux.HandleFunc` in
`go/internal/query/` and `go/internal/serviceintelhttp/`. Each route must have a
matching OpenAPI 3.0 fragment in `go/internal/query/openapi_paths_*.go`. Today
there is no automated check that the two surfaces agree. Drift between registered
routes and documented paths can accumulate silently.

## Decision

Add a self-contained bash verifier (`scripts/verify-openapi.sh`) that extracts
both sets — HandleFunc routes and OpenAPI path entries — and exits non-zero on
any drift. Wire the verifier into CI.

### Verifier design

**Input surfaces:**
- HandleFunc registrations in `go/internal/query/*.go` and
  `go/internal/serviceintelhttp/*.go` (excluding `*_test.go` and `openapi_*.go`)
- OpenAPI path definitions in `go/internal/query/openapi_paths_*.go`

**Three HandleFunc patterns are handled:**

1. Direct string literal: `mux.HandleFunc("GET /path", ...)`
2. Route constant reference: `const r = "GET /path"; mux.HandleFunc(r, ...)`
3. String concatenation: `const p = "/path"; mux.HandleFunc("POST "+p, ...)`

**OpenAPI extraction** uses an awk depth-aware parser that handles paths with
multiple HTTP methods (GET + POST on the same endpoint).

**Exit codes:**
- `0` — HandleFunc routes and OpenAPI entries are identical (clean)
- `1` — drift detected (missing or orphan entries reported)

### Regenerator (optional)

A Go tool at `cmd/openapi-generator/main.go` may walk HandleFunc registrations
and emit `openapi_paths_*.go` files. It is optional and not wired into the
default build.

### CI gate

The verifier runs on every PR and push to `main` via
`.github/workflows/verify-openapi.yml`. A red gate blocks merge until the drift
is resolved.

### Known-drift exclusions

`.github/openapi-known-drift.txt` lists routes that are intentionally,
**permanently** not part of the OpenAPI surface — documentation UIs, and
routes whose METHOD or path is computed at runtime or lives in a package
outside the verifier's scan directories. One route per line in `METHOD /path`
format; comments start with `#`. The verifier subtracts these from the drift
report via `comm -23` so the CI gate stays green on acknowledged, permanent
exclusions while catching new drift.

**The file is not a backlog (#5762 correction).** The original design
(above) let a route sit here because it was a "pre-existing gap," on the same
footing as a genuine permanent exclusion. That third, unstated category is
exactly what let `POST /api/v0/code/visualize` sit here for the six weeks
since #3781 was filed under `# TODO(#3781): add openapi_paths_code_visualize
fragment for this route.`, invisible to the drift gate that would otherwise
have forced the fragment to be written (see
[Graph-read safety](../../public/reference/telemetry/graph-read-safety.md)
for the full incident). An entry in this file must assert "this route is not
OpenAPI," never "the OpenAPI fragment has not been written yet" — the second
claim means the route belongs in `openapi_paths_*.go`, not here.
`scripts/verify-openapi.sh` now enforces that distinction structurally,
before it evaluates route drift, and fails closed if any of the four checks
below trips:

1. **No deferral markers, on a comment line.** No comment line in the file
   may contain a "fix this later" style marker (TODO — including the
   hyphenated/underscored/spaced "TO-DO" spelling — FIXME, XXX, HACK, TBD,
   WIP, case-insensitive, including plural forms such as "TODOs"). Such a
   marker is self-refuting: it asserts the route is fixable, which
   contradicts "permanent, intentional exclusion." This is a closed,
   conventional token set and the check is exhaustive for those tokens. The
   check only scans comment lines, not route lines, so a route whose own
   path happens to contain one of these words (e.g. `/todo-board`,
   `/cache/wipe`) is never blocked by this rule. A justification comment may
   not itself use a marker word or a listed prose phrase to describe the
   route it excludes, even in passing — e.g. `/api/v0/todo-board` needs a
   justification reworded around a synonym ("kanban board"), never restating
   the route's own vocabulary, because rules 1 and 2 scan every comment line,
   including the justification itself.
2. **No prose deferral phrases, on a comment line.** No comment line may
   contain a deferral claim spelled out in words instead of a marker —
   shapes like "not written", "written yet", "pending", "predate(s)",
   "later", or "to be added/written". This is the same self-refuting signal
   as rule 1; a route can defer itself just as effectively in prose as with
   a TODO. This rule is prospective, not retrospective: `POST
   /api/v0/code/visualize`'s real justification comment
   (`# TODO(#3781): add openapi_paths_code_visualize fragment for this
   route.`) used a TODO marker, so rule 1 alone would already have caught
   it — rule 2 exists so the next entry that defers itself in prose,
   without ever using a marker word, does not slip past rule 1 the way this
   one would have.
   **This rule is a best-effort convenience, not a closure guarantee.** The
   phrase list is fixed and finite; an English deferral phrased outside it
   will not be caught, and each review round of #5762 found more surviving
   shapes ("Deferred:", "Revisit once…", "Backlog item…", and others). Do
   not extend this list round after round expecting it to eventually close —
   it cannot, the same way the trivy-gate bash-parsing denylist could not.
   Rule 1 is the closed, defensible guarantee; rule 2 is a convenience on
   top of it.
3. **Every entry must be justified on its own, with real words.** Each route
   line must be preceded by its own non-empty, substantive comment line
   explaining why it belongs in this file: at least two whitespace-separated
   tokens, including one alphabetic word of 4+ characters, so decoration
   such as `####` or `# ---` cannot pass as a justification. A blank line or
   a route line resets the justification state, so neither a bare route, a
   bare `#`, nor a comment shared across a group of routes can justify more
   than the one route line immediately following it.
4. **A justification may not be byte-identical to the one before it.** Rule
   3's "cannot be shared across a group of routes" was enforced only by
   position, so a copy-pasted duplicate still counted as each route having
   "its own" comment (#5762 round 6, F14). Give each route its own wording,
   even when the underlying reason is the same for both.

`scripts/lib/test-verify-openapi-known-drift-cases.sh` and
`scripts/lib/test-verify-openapi-known-drift-hardening-cases.sh` (both
sourced by `scripts/test-verify-openapi.sh`) carry synthetic fixtures for all
four rules: a TODO-marked entry, its plural form, the TO-DO spelling, the
HACK/TBD/WIP markers, two prose-deferral shapes, a bare unjustified entry, a
bare `#` comment, two decoration-only comments (`####`, `# ---`), a route
riding in immediately after an already-justified route, a well-justified
entry with no deferral marker, a route whose own path contains a marker word
(`/todo-board`) proving the comment-only scan does not block a legitimately
excluded route, a route (`/cache/wipe`) whose real justification comment
contains the marker substring "wipes," proving the trailing word-boundary
guard stops the marker rule from false-tripping on it, a leading-whitespace
route line still trimmed and subtracted from the drift diff, and a fixture
pinning the exact wording of the unjustified-entry failure message. The
round-6 file adds: fixtures that isolate each half of rule 3's token-count
and 4+-letter-word conjunction, the previously-unfixtured FIXME and XXX
markers, a lowercase marker (proving the case-insensitive match), five
prose alternatives that were never independently pinned ("not written",
"written yet", "not yet written", "predate", "later" — the "predate" case
also proves the singular/plural fix), an `rg`-hard-error case proving the
known-drift scan fails closed instead of reporting a false-clean gate, and a
duplicate-justification case for rule 4.

**Dynamic-route escape hatch:** the verifier resolves route constants and string
concatenation (patterns 1b/1c) via regex, not AST. A route whose METHOD or path
is constructed at runtime (e.g. built from a non-const function return, or whose
const lives in a package the verifier does not scan) cannot be resolved. Such
routes must be added to the known-drift file with a comment explaining why the
scanner cannot resolve them. Do not weaken the matcher to chase dynamic routes
— the file is the explicit escape hatch for genuinely unresolvable routes, not
for routes that are merely undocumented yet.

## Consequences

- Every new or changed API route must update its `openapi_paths_*.go` fragment
  before the CI gate passes.
- V-1 (#3813, `/api/v1/` alias) must land AFTER X2-1 is merged so the verifier
  gates V-1's OpenAPI changes correctly.
- Known intentional exceptions (`/docs`, `/redoc` — documentation UIs) are
  caught by the verifier as drift. They are not exempted; the verifier forces an
  explicit decision.
- The verifier is self-contained (bash + rg + awk), requiring no Go compilation
  or external dependencies beyond what is already present in CI.
- Routes whose METHOD/path is computed at runtime or lives outside scan dirs
  must be listed in `.github/openapi-known-drift.txt`.
- A known-drift entry may never carry a deferral marker, a prose deferral
  phrase, lack a preceding justification comment, or repeat the immediately
  preceding justification byte-for-byte (#5762) — `scripts/verify-openapi.sh`
  fails the whole gate on any of those four shapes before it evaluates route
  drift.

## Verification

```bash
# Unit tests for the verifier (synthetic fixtures)
bash scripts/test-verify-openapi.sh

# Full verifier on the current tree
bash scripts/verify-openapi.sh
```
