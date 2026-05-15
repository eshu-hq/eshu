# Eshu Console

`apps/console` is the private read-only product console for Eshu. It is
separate from the root Cloudflare Pages homepage.

The console is role-neutral at the front door: search for a repository, service,
or workload, then open an entity workspace with story, evidence, deployment,
code, findings, and freshness context.

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
state, findings, and future security posture.

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

## First Slice

PR 1 includes:

- local Eshu API default with explicit demo-mode fallback
- typed envelope client
- role-neutral search
- repository story workspace from the live HTTP API
- dashboard, catalog, and findings surfaces
- dead-code as the first finding type

Future slices can add D3 path maps and uiGrid-backed dense tables behind the
existing `visualization/` and `grid/` component boundaries.
