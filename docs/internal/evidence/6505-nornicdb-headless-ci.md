# #6505 — NornicDB image builds headless in Ifa gates

`main` went red when the exact-source NornicDB pin (`3722b483`, introduced in
b064f8e3e) started building upstream's `docker/Dockerfile.cpu-bge` UI stage in
CI: `npm run build` (`tsc && vite build`) fails with TS2882 on side-effect CSS
imports (`./Bifrost.css`, `./index.css`). Eshu owns no `ui/` or `docker/`
tree — the failure is upstream's tree at our pin plus our default from-source
build policy (`pull_policy: build`). Sibling workflows stay green because they
never build this image from source.

The fix threads upstream's own documented `HEADLESS` build arg through
`docker-compose.yaml` (`args: HEADLESS: ${NORNICDB_HEADLESS:-false}`; local
default `false` keeps the full UI build) and sets `NORNICDB_HEADLESS=true` on
the three image-building jobs in `ifa-determinism-gate.yml`
(determinism-matrix, dead-letter-matrix, fault-injection shards). The backend
pin — including the #261/#290 fixes — is untouched.

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
