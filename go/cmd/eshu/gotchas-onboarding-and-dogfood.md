# eshu CLI Gotchas — Onboarding And Dogfood Commands

Split out of [`README.md`](README.md) for issue #6059 so that file stays under
the repository's 500-line cap. This page carries the command contracts for
`scan`, `first-run`, `hosted-setup`, `hosted-onboard`, `first-run-benchmark`,
and `answer-quality-scorecard` — the path a new operator walks from a checkout
to one bounded answer, plus the two commands that score that path afterwards.
The root-command invariants and the pointer back to these pages stay in
`README.md`.

## Invariants

- `eshu scan` is the readiness contract for one local source. It preflights the
  configured API status surface, launches `eshu-bootstrap-index` with
  `ESHU_REPO_SOURCE_MODE=filesystem`, `ESHU_FILESYSTEM_ROOT` set to the
  resolved source root, `ESHU_FILESYSTEM_DIRECT=true`, and `ESHU_REPOS_DIR`
  under the workspace cache, then polls `/api/v0/status/pipeline` until health
  is `healthy`, queue work is drained, no failures or dead letters exist, and at
  least one generation completed. It also probes `/api/v0/repositories?limit=1`
  before and after the run so the API query surface has to respond, not just the
  status store. It reports bootstrap and queue-zero timings.
  Collector-complete and source-local projection-complete timings remain
  explicit `null` values in JSON because the bootstrap child logs those events
  today but does not expose parent-process structured timestamps.
- `eshu first-run [path]` is the guided onboarding contract. It runs discrete,
  individually testable steps: detect the runtime shape (a reachable API wins,
  then local `eshu-*` binaries on `PATH`, then a `docker-compose.yaml` at the
  workspace root), verify the runtime is usable without performing any
  destructive auto-start, index the target repository (reusing an existing
  drained index when one already serves the target) or run `eshu scan`, wait for
  indexing completeness through the shared `evaluateScanReadiness` logic rather
  than process health, then run one bounded `/api/v0/repositories?limit=5`
  query. It reports overall success only when that bounded query actually
  returns; readiness or process health alone never counts as success. Failure
  paths preserve the underlying error and print actionable next steps. `--json`
  emits the canonical `{data, truth, error}` envelope; `--no-start` is a safe
  mode that only verifies and reports.
- `eshu hosted-setup` is the first-five-minutes contract for a *deployed*
  service. It resolves the endpoint and bearer token through the shared remote
  flags (`--service-url`/`ESHU_SERVICE_URL`, `--api-key`/`ESHU_API_KEY`, then
  persisted config) and runs ordered, individually-reported stages: endpoint and
  auth resolved, `/healthz`, `/readyz` (which also proves authentication),
  status/index readiness via the shared `evaluateScanReadiness` logic, MCP tool
  visibility, and one bounded `/api/v0/repositories` query. Each failure carries
  a distinct category — `auth-unavailable`, `unreachable`, `empty-index`,
  `partial-readiness`, `stale-readiness`, `missing-repo-scope`, or
  `mcp-unavailable` — so the operator sees exactly which stage failed and why
  without reading every deployment page. It reports connected only when the
  bounded query actually returns; health or readiness alone never counts. The
  raw bearer token is never printed: output carries only a redacted token
  reference (the `${ESHU_API_KEY}` env reference for snippet-capable platforms,
  otherwise a masked placeholder). `--platform` emits a hosted MCP client
  snippet via the shared `mcp setup` snippet helpers; `--repository` asserts a
  required repository is present in the indexed scope; `--json` emits the
  canonical `{data, truth, error}` envelope.
- `eshu hosted-onboard` is the shared-service onboarding contract for a
  *deployed* service. It takes a required `--team` name and a repository sync
  rule set (`--repo owner/name`, repeatable, and `--repo-pattern '^org/team-'`,
  repeatable). It classifies the rule set narrow vs broad through the pure
  `classifyRepoRules` function and rejects an accidental whole-org glob (`org/*`,
  `*`, `.*`, an empty rule set) before any connection check runs unless
  `--confirm-broad` is supplied. It then reuses the `hosted-setup` staged checks
  and projects a redacted onboarding artifact: the API URL, the
  `<base>/mcp/message` MCP URL (both endpoint-redacted), the token *source name*
  (the `ESHU_API_KEY` env var, never the value), the indexed repositories, a
  queue/completeness status derived from the readiness verdict, and starter
  prompts plus `starter_playbooks[]` sourced from the query playbook catalog.
  Each structured starter playbook names the playbook ID, version, prompt
  family, ordered tools, and expected answer truth classes. The artifact
  documents the current single shared-token authorization limitation so it never
  implies per-team isolation that does not exist. `--out <path>` with
  `--format md|json` writes the artifact with owner-only permissions; `--json`
  prints it to stdout; `--platform` adds a hosted MCP client snippet. Like
  `hosted-setup`, the exit code reflects whether the bounded query actually
  returned.
- `eshu first-run-benchmark` is the dogfood benchmark contract. It consumes a
  captured `first-run --json` envelope (from `--envelope <path>` or stdin) and
  scores it against the first-five-minutes onboarding criteria through the pure
  `firstrunbench.Evaluate` function (`go/internal/cli/firstrunbench`). The
  benchmark exits non-zero, and the verdict is FAIL, whenever the "first
  answer" is health-only: no bounded query returned, missing truth metadata,
  missing source handle, incomplete indexing, or an error envelope. Optional criteria (time-to-answer, manual-step count)
  record honest `not_measured` values rather than fabricated numbers and never
  flip an otherwise-complete run to FAIL.
- `eshu answer-quality-scorecard` is the broader answer dogfood contract. It
  consumes a captured, redacted `answer-quality-scorecard/v1` artifact from
  `--from <path>` or stdin and scores representative service-story,
  code-topic, incident-context, supply-chain, documentation-truth,
  freshness/readiness, and hosted-governance prompts. It exits non-zero when
  family coverage, usefulness, truth honesty, citation coverage, boundedness,
  narration fallback preservation, parity, follow-up usefulness, or publish
  safety fails. The command never captures live answers itself; callers must
  capture real API/MCP/CLI/hosted outputs, redact them, then score the artifact.
