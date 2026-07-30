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
- **Recognized evidence-file locations**: `docs/public/adrs/*.md`,
  `docs/public/reference/**/*.md`, `docs/internal/evidence/**/*.md`,
  `docs/internal/design/**/*.md`, and any `.md` file directly under a
  `go/**` or `sdk/go/**` package directory — not just `README.md`,
  `AGENTS.md`, or `evidence-*.md`. The repo's real convention already has
  evidence recorded in topic-named package docs such as
  `go/internal/query/read-models.md`,
  `go/internal/storage/postgres/gotchas-and-invariants.md`, and
  `sdk/go/factschema/README.md`; any of these locations works as long as
  the marker is in this PR's own added lines. A `.md` file under any
  `testdata/`, `vendor/`, or `generated/` directory (at any depth, e.g.
  `go/cmd/audit-preflight/testdata/*.md`) is never a recognized evidence
  location — those are fixtures or third-party/generated content, not
  hand-authored documentation, and the gate excludes them before matching
  the whitelist above (eshu-hq/eshu#5542 follow-up).
