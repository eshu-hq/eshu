# #6505 — NornicDB image builds headless in Ifa gates

`main` went red when the exact-source NornicDB pin (`3722b483`, introduced in
b064f8e3e) started building upstream's `docker/Dockerfile.cpu-bge` UI stage in
CI: `npm run build` (`tsc && vite build`) fails with TS2882 on side-effect CSS
imports (`./Bifrost.css`, `./index.css`). Eshu owns no `ui/` or `docker/`
tree — the failure is upstream's tree at our pin plus our default from-source
build policy (`pull_policy: build`).

CORRECTION (follow-up): the sibling workflows do NOT stay green by immunity.
`docker-compose.e2e.yaml` extends the same `nornicdb` service and the
golden-corpus nornicdb leg uses the same file, so e2e, golden-corpus, and the
frontend e2e jobs all build the identical image — they were green only via
path-filter skips and a lucky `npm ci`. The knob is therefore threaded through
those jobs too (see below), not just the Ifa gate.

Root cause, refined: the break is intermittent, not per-pin. Upstream's UI
stage runs `npm ci 2>/dev/null || npm install --legacy-peer-deps`: when `npm
ci` fails (stderr hidden), the fallback resolves floating `^` ranges to newer
versions and the tree breaks (failed Ifa run: "added 203 packages, changed 1
package" in 16s). When `npm ci` succeeds, the lockfile tree builds clean
(passing e2e run on the same pin: full `tsc && vite build` green). Skipping
the UI stage removes the flake surface entirely rather than depending on
registry luck.

The fix threads upstream's own documented `HEADLESS` build arg through
`docker-compose.yaml` (`args: HEADLESS: ${NORNICDB_HEADLESS:-false}`; local
default `false` keeps the full UI build) and sets `NORNICDB_HEADLESS=true`
everywhere CI builds the image: step-level env on the three image-building
jobs in `ifa-determinism-gate.yml` (determinism-matrix, dead-letter-matrix,
fault-injection shards); job-level env on the e2e `test` job, the
golden-corpus `corpus-gate` job, and the value-flow `expectation` job; and
workflow-level env in `frontend.yml`, which covers the `auth-sso-e2e`
(`run-auth-e2e.sh`) and `auth-mcp-e2e` (`run-auth-mcp-e2e.sh`) jobs that do
fresh `up --build` (the `console` job itself builds nothing — mocked browser
tests and static harness checks only). The backend
pin — including the #261/#290 fixes — is untouched. A pinning test asserts
the per-file assignment counts (3/1/1/1/1, assignment literals so prose
mentions cannot inflate them) so a later edit cannot silently re-arm
the flake. Out of scope (operator-driven, no CI job): the k8s/two-team,
remote-OCI, console-retained, and demo scripts that also `up --build`; their
operators inherit the local default unless they export the knob.

No-Regression Evidence (#6505): backend behavior is unchanged. The pin, build
context, dockerfile, Go builder stage, and every backend flag and env are
byte-identical; only the UI static bundle is skipped when the knob is set, and
no gate serves the NornicDB UI (all three jobs assert graph determinism,
dead-letters, and recovery over Bolt/HTTP only).
Baseline: the CI image build fails at the UI `tsc` step (TS2882, Ifa
Determinism Gate run 33804850327, job 100812753233; same workflow failed on
`main@29b91b8a39`).
After: the upstream `ui` stage builds headless from the exact pinned context
in 13.8s locally
(`docker build <pin-context> -f docker/Dockerfile.cpu-bge --target ui
--build-arg HEADLESS=true`, exit 0), and `docker compose config` renders
`HEADLESS: "false"` by default and `"true"` with the knob set. Full-stack
proof is the Ifa gate itself going green on the PR.

No-Observability-Change: no metric, span, log, or status surface moves.
