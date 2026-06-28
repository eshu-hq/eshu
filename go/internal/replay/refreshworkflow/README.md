# refreshworkflow

Local proofs for the R-6 credentialed cassette refresh workflow (epic #4102,
issue #4108).

## Why

The refresh workflow itself runs on a hosted GitHub Actions runner with real
provider credentials. It cannot run locally without live API access. However,
the two properties that make the refresh workflow safe and reviewable are fully
provable without credentials:

1. **Canonical-diff legibility** — a single field change produces one changed
   line in the diff, not whole-file churn. This property holds because cassettes
   are canonicalized (sorted keys, sorted arrays, volatile fields collapsed). A
   re-record of an unchanged provider API produces an empty diff; a re-record
   after a fact-shape change produces a small, readable delta.

2. **Redaction** — secrets never appear in the recorded artifacts. The recorder
   calls `replay.Canonicalize` with `WithRedactedKeys` so credential-bearing
   payload fields are replaced with `"<redacted>"` before the cassette is
   written.

## Tests

Run with:

```
cd go && go test ./internal/replay/refreshworkflow -count=1
```

All tests are offline and credential-free. They exercise the real
`replay.Canonicalize` path, not a mock, so they fail when the canonical
properties break.

| Test | Proves |
| --- | --- |
| `TestCanonicalDiffIsLegible` | Single fact change → exactly one changed line in the diff |
| `TestRedactionNeverLeaksSecrets` | Configured secrets replaced at all depths; sentinel present; non-secrets preserved |
| `TestCanonicalFormIsStableAcrossReRecord` | Re-canonicalizing a canonical cassette is byte-identical (no spurious diffs) |

## What the credentialed CI job does

The `refresh-cassettes.yml` workflow is triggered by applying the
`[refresh-cassettes]` label to a PR or by `workflow_dispatch`. It:

1. Checks out the repository on a hosted runner with the provider secrets.
2. Builds each collector binary (`go build ./cmd/collector-*`).
3. Runs each collector in `-mode=record -cassette-file=<path>` to regenerate
   the committed cassettes under `testdata/cassettes/`.
4. Commits the regenerated cassettes on a branch and opens (or force-updates)
   a PR titled `chore(replay): cassette refresh (R-6, #4108)`.
5. The diff in that PR is the canonical diff — reviewable, line-level, safe to
   commit because secrets are redacted by the recorder before the cassette is
   written.

## See also

- `go/internal/replay/recorder` — the `recorder.Run` function the workflow
  invokes via `-mode=record`.
- `go/internal/replay/canonical.go` — `Canonicalize` and `DefaultCanonicalOptions`.
- `.github/workflows/refresh-cassettes.yml` — the label-gated CI workflow.
- `docs/internal/design/4102-deterministic-replay-framework.md` §6 — R-6
  scope and design rationale.
