# Documentation gate scope

The citation checker previously read test and fixture citations only from
public language pages and the parity matrix. The Markdown cap evaluated only
`go/`. Both now include Markdown under `docs/`, including internal evidence.
TEST and FIXTURE scans also traverse hidden and ignored directories under `docs/`.
The cap still excludes fixture, generated, vendor, and hidden path segments.

## Regression proof

The script test mirrors exercise the production CLI with isolated trees:

- `bash scripts/test-verify-doc-citations.sh --scope-only` initially exited 1:
  seeded missing tests and fixtures passed with zero checked citations.
  After the fix, those citations fail with the document and target named;
  removing each violation restores green. Valid citations report nonzero counts.
- `bash scripts/test-verify-markdown-line-cap.sh --scope-only` initially exited
  1: 501-line internal and public docs passed with zero evaluated files under
  both `--all` and `--files`. They now fail, while reducing to 500 lines or
  removing the file restores green.
- The cap tests also exercise ledger imports against a real immutable base.
  Existing oversized docs may import their base count or a smaller count;
  new paths, renamed paths, formerly compliant files, and inflated pins fail.

## Existing debt

`scripts/lib/markdown-line-cap-grandfather.tsv` lists the oversized documents
that predate this scope expansion. The gate verifies each newly imported docs
pin against the file at the resolved immutable base, so this change cannot
exempt a new file or branch-authored growth. Existing pins retain the cap's
prior policy: shrinking is allowed; growth above the pin is rejected. A shrink
may regrow below its pin until the pin is lowered. Remove the row when the
file is split below the cap. This avoids rewriting documents owned by active
package-move lanes while enforcing the cap on their next changes.

The citation ledger retains its existing value-based fixture debt policy.
The newly scanned design and reference pages contain planned or unused fixture
references; their existing values are recorded in the ledger. A new unlisted
missing test or fixture fails. A citation to an already baselined fixture is
still allowed anywhere; this scope change does not strengthen that old policy.
TEST keys written by the updater are now relative to `docs/`; old language-page
and parity-matrix keys remain readable. Raw line-citation authority is unchanged.

## Integration

The pre-commit path filter and gate registry include docs Markdown. A dedicated
unconditional CI job invokes the same Markdown cap and self-tests, including
on docs-only PRs that skip the Go lanes. The workflow regression rejected the
previous code-only job placement before the fix, and rejects missing jobs,
conditional jobs or steps, and missing or commented-out commands. The generated gate reference is refreshed from
the registry. No reducer, query, saved replay input, or golden snapshot changes
are part of this fix.

The existing `verify-contracts` job retains the cap and its self-tests during
rollout: the trusted required-gates publisher reads the default branch's
registry, which still names that job when this PR first runs. Regression cases
reject removal or commenting of those invocations even when the dedicated job
contains identical commands. Hidden-directory citation cases track pages in
Git, reject missing TEST/FIXTURE targets by name, and verify removal restores
green and valid citations produce nonzero counts. Additional force-tracked
cases cover directory exclusions in `.gitignore`, `.ignore`, and `.rgignore`
under ordinary and hidden paths: each missing target failed only after both
scans disabled ignore filtering. Valid replacements report nonzero counts.
