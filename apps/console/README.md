# Eshu Console

`apps/console` is the private read-only product console for Eshu. It is
separate from the root Cloudflare Pages homepage.

The console is role-neutral at the front door: search for a repository, service,
or workload, then open an entity workspace with story, evidence, deployment,
code, findings, and freshness context.

## Product Contract

Eshu Console turns Eshu's code-to-cloud graph into a readable operating
surface. It serves engineers, platform teams, SREs, support, directors,
executives, and finance-adjacent stakeholders who need the same graph truth at
different depths.

The console should help users answer:

- what a repository, service, or workload does
- what deploys it, where it runs, and what depends on it
- what evidence supports each relationship
- whether indexing is healthy, stale, partial, or blocked
- which findings need action
- which replatforming/import-plan candidates are ready, refused, or missing
  ownership evidence
- what is known, missing, inferred, or stale
- which cloud resources are unmanaged, drifting, or blocked from safe import

Important claims need a plain-language summary first and a clear path to the
underlying evidence.

## Design Contract

The console is a working product surface, not a homepage or generic dashboard.
Keep it bright, readable, and dense enough for repeated operational use. Use
color for relationship type, status, freshness, risk, and selection; never use
color as decoration or the only state indicator.

Design rules:

- show the story and the proof together
- keep graph labels and table rows readable before adding visual polish
- preserve truth, freshness, missing-evidence, and limitation states in text
- avoid dark-card sameness, decorative gradients, glass effects, nested cards,
  modal-first drilldowns, and mock-data language on real-data surfaces
- keep non-graph summaries available when graph views are not the fastest way
  to understand the evidence

## Modes

### Demo mode

Demo mode uses typed fixtures only. It is explicit, not the local default. Use
it for public demos, screenshots, and development when no Eshu API is running.

Demo mode must not imply that public users can browse real Eshu data.

### Private real-data mode

Private mode points the console at an Eshu API base URL, such as a local Compose
stack or an internal deployment.

The local development default is `/eshu-api/`, which the console Vite server
proxies to `http://127.0.0.1:8080`. Start a local Eshu API before opening the
console if you want real data.

Until Eshu has real auth and authorization, real-data console deployments should
stay local or inside a trusted private network. The console is read-only, but
the data can still expose repositories, services, infrastructure, docs, runtime
state, findings, and security posture.

No mutating controls belong in this app until auth, audit logging, confirmation,
and role policy exist.

## Local Development

From the repository root:

```bash
npm run console:test
npm run console:typecheck
npm run console:build
```

Run the console dev server:

```bash
npm run --prefix apps/console dev
```

The root helper scripts run against the console Vite config while sharing the
repository lockfile and dependency install.

## API Contract

The console treats `application/eshu.envelope+json` as the canonical response
contract. Screen code must preserve:

- `truth.level`
- `truth.profile`
- `truth.freshness.state`
- structured `error.code`
- limits, truncation, and unsupported-capability states when an API returns
  them

Do not flatten truth and freshness into generic loading or error states.

Cloud drift surfaces use bounded POST readbacks:

- `POST /api/v0/cloud/runtime-drift/findings`
- `POST /api/v0/aws/runtime-drift/findings`
- `POST /api/v0/iac/unmanaged-resources`
- `POST /api/v0/iac/management-status/explain`
- `POST /api/v0/iac/terraform-import-plan/candidates`

The console must render safety gates, missing evidence, pagination, and refused
candidate state as read-only context. It must not emit Terraform HCL, run
Terraform, import resources, or mutate cloud state.

## Related Docs

- `docs/public/reference/http-api.md`
- `docs/public/reference/truth-label-protocol.md`
- `docs/public/guides/visualization.md`
