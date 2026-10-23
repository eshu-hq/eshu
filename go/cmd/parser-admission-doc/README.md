# parser-admission-doc

Regenerates the admission table in
`docs/public/languages/admitted-files.md` from the parser registry, so the
documented list of admitted file names is data that cannot drift from the code.

## Use

```bash
scripts/generate-parser-admission-doc.sh           # rewrite the table
scripts/generate-parser-admission-doc.sh --check   # fail if the page is stale
```

The command needs `-repo-root`; the script supplies it. It replaces only the
text between the `parser-admission-table` markers and leaves the rest of the
page alone. A page without the markers is an error.

## Contract

- Input: `parser.DefaultRegistry()` (`Ecosystem` and `Admission` per definition).
- Output is deterministic and byte-idempotent.
- The drift gate is `TestAdmissionTableDocIsCurrent` in `go/internal/parser`,
  which runs with the normal parser tests, and `-check` here.
- Build-time only. No storage, graph, network, or telemetry dependency.
