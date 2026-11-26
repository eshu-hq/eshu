# Performance Evidence Gate

`scripts/verify-performance-evidence.sh` blocks a PR that touches hot Cypher,
graph writes, queues, workers, leases, batching, or runtime knobs unless the
same PR carries its own tracked evidence:

- **Evidence must be in the PR's own added lines**, not merely present
  somewhere in a touched file. A marker that already existed in the file
  before this PR — added by an earlier, unrelated PR — does not satisfy the
  gate even if the PR happens to touch that file for something else. Add a
  fresh `Performance Evidence:`/`Benchmark Evidence:`/`No-Regression
  Evidence:` line (and a fresh `Observability Evidence:`/
  `No-Observability-Change:` line) as part of this PR's own diff. An
  optional single parenthetical or bracketed qualifier between the marker
  phrase and the colon is accepted (`Performance Evidence (#1234): ...`,
  `No-Regression Evidence [tracked in ISSUE-1234]: ...`) — an established
  convention already used across dozens of files — but the colon is always
  required; a bare mention of the phrase with no colon does not count.
- **Recognized evidence-file locations**: prefer the owning package doc under
  `go/**` or `sdk/go/**` (for example `go/internal/query/read-models.md`,
  `go/internal/storage/postgres/gotchas-and-invariants.md`, or
  `sdk/go/factschema/README.md`), an ADR under `docs/public/adrs/*.md`, a
  reference doc under `docs/public/reference/**/*.md`, a design doc under
  `docs/internal/design/**/*.md`, or a `workflow-artifact:` pointer. Only use
  `docs/internal/evidence/*.md` for durable live-environment baselines that
  follow `docs/internal/evidence/README.md` (Markdown only, ≤150 lines, plain
  English `## Summary`, no raw dumps). A `.md` file under any `testdata/`,
  `vendor/`, or `generated/` directory (at any depth, e.g.
  `go/cmd/audit-preflight/testdata/*.md`) is never a recognized evidence
  location — those are fixtures or third-party/generated content, not
  hand-authored documentation, and the gate excludes them before matching
  the whitelist above (eshu-hq/eshu#5542 follow-up).
